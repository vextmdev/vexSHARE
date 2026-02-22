package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterAllow(t *testing.T) {
	l := New(3, 1*time.Minute)
	ip := "192.168.1.1"
	for i := 0; i < 3; i++ {
		if !l.Allow(ip) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
	if l.Allow(ip) {
		t.Error("4th request should be denied")
	}
	if !l.Allow("10.0.0.1") {
		t.Error("different IP should be allowed")
	}
}

func TestLimiterReset(t *testing.T) {
	l := New(2, 1*time.Minute)
	ip := "5.6.7.8"
	l.Allow(ip)
	l.Allow(ip)
	if l.Allow(ip) {
		t.Error("should be rate limited")
	}
	l.Reset(ip)
	if !l.Allow(ip) {
		t.Error("should be allowed after reset")
	}
}

func TestLimiterWindowExpiry(t *testing.T) {
	l := New(1, 50*time.Millisecond)
	ip := "9.9.9.9"
	if !l.Allow(ip) {
		t.Error("first should be allowed")
	}
	if l.Allow(ip) {
		t.Error("second should be denied")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.Allow(ip) {
		t.Error("should be allowed after window")
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name, addr, xff, want string
	}{
		{"ip:port", "192.168.1.1:12345", "", "192.168.1.1"},
		{"just ip", "192.168.1.1", "", "192.168.1.1"},
		{"xff single", "10.0.0.1:1234", "203.0.113.50", "203.0.113.50"},
		{"xff multi", "10.0.0.1:1234", "203.0.113.50, 70.41.3.18", "203.0.113.50"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.addr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := ExtractIP(req); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	l := New(2, 1*time.Minute)
	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.1.1.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}
