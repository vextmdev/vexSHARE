package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type Config struct {
	Mode     string
	Username string
	Password string
	Token    string
	Secure   bool
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionEntry
	ttl      time.Duration
}

type sessionEntry struct {
	createdAt time.Time
	username  string
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	s := &SessionStore{
		sessions: make(map[string]sessionEntry),
		ttl:      ttl,
	}
	go s.cleanup()
	return s
}

func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for k, v := range s.sessions {
			if now.Sub(v.createdAt) > s.ttl {
				delete(s.sessions, k)
			}
		}
		s.mu.Unlock()
	}
}

func (s *SessionStore) Create(username string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	id := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[id] = sessionEntry{
		createdAt: time.Now(),
		username:  username,
	}
	s.mu.Unlock()
	return id, nil
}

func (s *SessionStore) Valid(id string) bool {
	s.mu.RLock()
	entry, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Since(entry.createdAt) <= s.ttl
}

func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func CheckPassword(cfg Config, username, password string) bool {
	userOk := subtle.ConstantTimeCompare([]byte(cfg.Username), []byte(username)) == 1
	passOk := subtle.ConstantTimeCompare([]byte(cfg.Password), []byte(password)) == 1
	return userOk && passOk
}

func CheckToken(cfg Config, token string) bool {
	return subtle.ConstantTimeCompare([]byte(cfg.Token), []byte(token)) == 1
}

const sessionCookieName = "vexshare_session"

func SetSessionCookie(w http.ResponseWriter, sessionID string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}

func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func GetSessionID(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func PasswordMiddleware(sessions *SessionStore, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sid := GetSessionID(r)
			if sid != "" && sessions.Valid(sid) {
				next.ServeHTTP(w, r)
				return
			}
			logger.Debug("unauthenticated request, redirecting to login", "path", r.URL.Path, "ip", r.RemoteAddr)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		})
	}
}

func TokenMiddleware(cfg Config, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.PathValue("token")
			if token == "" || !CheckToken(cfg, token) {
				logger.Warn("invalid token access attempt", "ip", r.RemoteAddr)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
