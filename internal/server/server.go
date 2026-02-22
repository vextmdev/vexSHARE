package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/vextm/vexshare/internal/auth"
	"github.com/vextm/vexshare/internal/ratelimit"
	"github.com/vextm/vexshare/internal/session"
	"github.com/vextm/vexshare/internal/ui"
)

type Config struct {
	ListenAddr  string
	TLSCert     string
	TLSKey      string
	AuthConfig  auth.Config
	SessionCfg  session.Config
	AllowOrigin string
	Logger      *slog.Logger
}

type Server struct {
	cfg        Config
	httpServer *http.Server
	sessions   *auth.SessionStore
	sess       *session.Session
	loginRL    *ratelimit.Limiter
	wsRL       *ratelimit.Limiter
	logger     *slog.Logger
	upgrader   websocket.Upgrader
}

func New(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		cfg:      cfg,
		sessions: auth.NewSessionStore(24 * time.Hour),
		loginRL:  ratelimit.New(5, 1*time.Minute),
		wsRL:     ratelimit.New(20, 1*time.Minute),
		logger:   logger,
	}

	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     s.checkOrigin,
	}

	return s
}

func (s *Server) checkOrigin(r *http.Request) bool {
	if s.cfg.AllowOrigin == "" {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		host := r.Host
		return strings.Contains(origin, host)
	}
	origin := r.Header.Get("Origin")
	for _, allowed := range strings.Split(s.cfg.AllowOrigin, ",") {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return false
}

func (s *Server) buildRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)

	loginHandler := s.loginRL.Middleware()(http.HandlerFunc(s.handleLoginPost))
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.Handle("POST /login", loginHandler)
	mux.HandleFunc("POST /logout", s.handleLogout)

	authMode := s.cfg.AuthConfig.Mode

	if authMode == "password" || authMode == "password+token" {
		pwMiddleware := auth.PasswordMiddleware(s.sessions, s.logger)
		mux.Handle("GET /", pwMiddleware(http.HandlerFunc(s.handleTerminal)))
		wsHandler := s.wsRL.Middleware()(pwMiddleware(http.HandlerFunc(s.handleWS)))
		mux.Handle("GET /ws", wsHandler)
	}

	if authMode == "token" || authMode == "password+token" {
		tokenMiddleware := auth.TokenMiddleware(s.cfg.AuthConfig, s.logger)
		mux.Handle("GET /t/{token}/", tokenMiddleware(http.HandlerFunc(s.handleTerminal)))
		wsHandler := s.wsRL.Middleware()(tokenMiddleware(http.HandlerFunc(s.handleWS)))
		mux.Handle("GET /t/{token}/ws", wsHandler)
	}

	if authMode == "token" {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Access requires a valid token URL.", http.StatusForbidden)
		})
	}

	return mux
}

func (s *Server) Start() error {
	sessCfg := s.cfg.SessionCfg
	sessCfg.Logger = s.logger
	sessCfg.OnClose = func() {
		s.logger.Info("PTY session ended, shutting down server")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.httpServer.Shutdown(ctx)
		}()
	}

	var err error
	s.sess, err = session.New(sessCfg)
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}

	handler := s.buildRouter()

	s.httpServer = &http.Server{
		Addr:         s.cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
		s.logger.Info("starting HTTPS server", "addr", s.cfg.ListenAddr)
		return s.httpServer.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
	}

	s.logger.Info("starting HTTP server", "addr", s.cfg.ListenAddr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.sess != nil {
		s.sess.Close()
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	staticFS, err := fs.Sub(ui.StaticFS, "static")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	data, err := fs.ReadFile(staticFS, "login.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	ip := ratelimit.ExtractIP(r)
	s.logger.Debug("login attempt", "username", username, "ip", ip)

	if !auth.CheckPassword(s.cfg.AuthConfig, username, password) {
		s.logger.Warn("failed login attempt", "username", username, "ip", ip)
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	sid, err := s.sessions.Create(username)
	if err != nil {
		s.logger.Error("create session", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	auth.SetSessionCookie(w, sid, s.cfg.AuthConfig.Secure)
	s.logger.Info("user logged in", "username", username, "ip", ip)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sid := auth.GetSessionID(r)
	if sid != "" {
		s.sessions.Delete(sid)
	}
	auth.ClearSessionCookie(w, s.cfg.AuthConfig.Secure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	staticFS, err := fs.Sub(ui.StaticFS, "static")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	data, err := fs.ReadFile(staticFS, "terminal.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Write(data)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", "error", err, "ip", ratelimit.ExtractIP(r))
		return
	}

	clientID := generateClientID()
	ip := ratelimit.ExtractIP(r)
	s.logger.Info("websocket connection", "client", clientID, "ip", ip)

	s.sess.AddClient(clientID, conn)
}

func generateClientID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
