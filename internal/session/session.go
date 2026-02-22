package session

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type resizeMsg struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type Client struct {
	ID           string
	Conn         *websocket.Conn
	IsController bool
	mu           sync.Mutex
}

func (c *Client) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.WriteJSON(v)
}

func (c *Client) WriteMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.WriteMessage(messageType, data)
}

type Session struct {
	cmd         *exec.Cmd
	ptmx        *os.File
	clients     map[string]*Client
	mu          sync.RWMutex
	sharedInput bool
	logger      *slog.Logger
	idleTimeout time.Duration
	lastActive  time.Time
	activeMu    sync.Mutex
	done        chan struct{}
	closeOnce   sync.Once
	onClose     func()
}

type Config struct {
	Command     string
	SharedInput bool
	IdleTimeout time.Duration
	Logger      *slog.Logger
	OnClose     func()
}

func New(cfg Config) (*Session, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("PTY is not supported on Windows")
	}

	shell := cfg.Command
	if shell == "" {
		shell = "bash"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Session{
		cmd:         cmd,
		ptmx:        ptmx,
		clients:     make(map[string]*Client),
		sharedInput: cfg.SharedInput,
		logger:      logger,
		idleTimeout: cfg.IdleTimeout,
		lastActive:  time.Now(),
		done:        make(chan struct{}),
		onClose:     cfg.OnClose,
	}

	go s.readPTY()
	if s.idleTimeout > 0 {
		go s.idleChecker()
	}

	return s, nil
}

func (s *Session) readPTY() {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if err != nil {
			if err != io.EOF {
				s.logger.Debug("pty read error", "error", err)
			}
			s.Close()
			return
		}
		s.touchActivity()
		data := make([]byte, n)
		copy(data, buf[:n])
		s.broadcast(data)
	}
}

func (s *Session) broadcast(data []byte) {
	encodedData, err := json.Marshal(string(data))
	if err != nil {
		s.logger.Error("marshal output data", "error", err)
		return
	}
	msg := wsMessage{
		Type: "output",
		Data: json.RawMessage(encodedData),
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		s.logger.Error("marshal broadcast", "error", err)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, c := range s.clients {
		if err := c.WriteMessage(websocket.TextMessage, raw); err != nil {
			s.logger.Debug("write to client failed", "client", id, "error", err)
		}
	}
}

func (s *Session) AddClient(id string, conn *websocket.Conn) *Client {
	s.mu.Lock()
	isController := len(s.clients) == 0
	c := &Client{
		ID:           id,
		Conn:         conn,
		IsController: isController,
	}
	s.clients[id] = c
	s.mu.Unlock()

	role := "viewer"
	if isController {
		role = "controller"
	}
	s.logger.Info("client connected", "id", id, "role", role)

	_ = c.WriteJSON(wsMessage{
		Type: "role",
		Data: json.RawMessage(fmt.Sprintf(`{"role":%q,"sharedInput":%v}`, role, s.sharedInput)),
	})

	s.broadcastClientCount()

	go s.readClient(c)
	return c
}

func (s *Session) readClient(c *Client) {
	defer s.RemoveClient(c.ID)
	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.logger.Debug("client read error", "id", c.ID, "error", err)
			}
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.logger.Debug("invalid message from client", "id", c.ID, "error", err)
			continue
		}

		switch msg.Type {
		case "input":
			if !s.canWrite(c) {
				continue
			}
			var input string
			if err := json.Unmarshal(msg.Data, &input); err != nil {
				continue
			}
			s.touchActivity()
			if _, err := s.ptmx.Write([]byte(input)); err != nil {
				s.logger.Debug("pty write error", "error", err)
				return
			}
		case "resize":
			var r resizeMsg
			if err := json.Unmarshal(msg.Data, &r); err != nil {
				continue
			}
			if err := pty.Setsize(s.ptmx, &pty.Winsize{
				Cols: r.Cols,
				Rows: r.Rows,
			}); err != nil {
				s.logger.Debug("pty resize error", "error", err)
			}
		}
	}
}

func (s *Session) canWrite(c *Client) bool {
	if s.sharedInput {
		return true
	}
	return c.IsController
}

func (s *Session) RemoveClient(id string) {
	s.mu.Lock()
	c, ok := s.clients[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	wasController := c.IsController
	delete(s.clients, id)

	if wasController && len(s.clients) > 0 {
		for _, next := range s.clients {
			next.IsController = true
			s.logger.Info("promoted client to controller", "id", next.ID)
			_ = next.WriteJSON(wsMessage{
				Type: "role",
				Data: json.RawMessage(`{"role":"controller"}`),
			})
			break
		}
	}
	s.mu.Unlock()

	s.logger.Info("client disconnected", "id", id)
	c.Conn.Close()
	s.broadcastClientCount()
}

func (s *Session) broadcastClientCount() {
	s.mu.RLock()
	count := len(s.clients)
	s.mu.RUnlock()

	msg := wsMessage{
		Type: "clients",
		Data: json.RawMessage(fmt.Sprintf(`{"count":%d}`, count)),
	}
	s.mu.RLock()
	for _, c := range s.clients {
		_ = c.WriteJSON(msg)
	}
	s.mu.RUnlock()
}

func (s *Session) touchActivity() {
	s.activeMu.Lock()
	s.lastActive = time.Now()
	s.activeMu.Unlock()
}

func (s *Session) idleChecker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.activeMu.Lock()
			idle := time.Since(s.lastActive)
			s.activeMu.Unlock()
			if idle > s.idleTimeout {
				s.logger.Warn("idle timeout reached, closing session", "idle", idle.Round(time.Second))
				s.Close()
				return
			}
		case <-s.done:
			return
		}
	}
}

func (s *Session) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.logger.Info("closing session")

		s.mu.Lock()
		for id, c := range s.clients {
			_ = c.Conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session closed"),
			)
			c.Conn.Close()
			delete(s.clients, id)
		}
		s.mu.Unlock()

		s.ptmx.Close()

		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		_ = s.cmd.Wait()

		if s.onClose != nil {
			s.onClose()
		}
	})
}

func (s *Session) Done() <-chan struct{} {
	return s.done
}
