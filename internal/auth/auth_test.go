package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckPassword(t *testing.T) {
	cfg := Config{Username: "admin", Password: "secret123"}
	tests := []struct {
		name     string
		user     string
		pass     string
		expected bool
	}{
		{"valid", "admin", "secret123", true},
		{"wrong pass", "admin", "wrong", false},
		{"wrong user", "nobody", "secret123", false},
		{"both wrong", "nobody", "wrong", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckPassword(cfg, tt.user, tt.pass); got != tt.expected {
				t.Errorf("CheckPassword(%q, %q) = %v, want %v", tt.user, tt.pass, got, tt.expected)
			}
		})
	}
}

func TestCheckToken(t *testing.T) {
	cfg := Config{Token: "my-secret-token"}
	if !CheckToken(cfg, "my-secret-token") {
		t.Error("expected valid token to pass")
	}
	if CheckToken(cfg, "wrong-token") {
		t.Error("expected invalid token to fail")
	}
}

func TestSessionStore(t *testing.T) {
	store := NewSessionStore(1 * time.Hour)
	sid, err := store.Create("testuser")
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if !store.Valid(sid) {
		t.Error("expected valid session")
	}
	if store.Valid("nonexistent") {
		t.Error("expected invalid session")
	}
	store.Delete(sid)
	if store.Valid(sid) {
		t.Error("expected deleted session to be invalid")
	}
}

func TestSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "test-id", false)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie")
	}
	c := cookies[0]
	if c.Name != sessionCookieName {
		t.Errorf("name: got %q, want %q", c.Name, sessionCookieName)
	}
	if c.Value != "test-id" {
		t.Errorf("value: got %q, want %q", c.Value, "test-id")
	}
	if !c.HttpOnly {
		t.Error("expected HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Error("expected SameSite=Lax")
	}
}

func TestClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w, false)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie")
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("MaxAge: got %d, want -1", cookies[0].MaxAge)
	}
}

func TestGetSessionID(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "my-session"})
	if got := GetSessionID(req); got != "my-session" {
		t.Errorf("got %q, want %q", got, "my-session")
	}
	req2 := httptest.NewRequest("GET", "/", nil)
	if got := GetSessionID(req2); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
