package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type Limiter struct {
	mu      sync.Mutex
	entries map[string]*entry
	limit   int
	window  time.Duration
}

type entry struct {
	timestamps []time.Time
}

func New(limit int, window time.Duration) *Limiter {
	l := &Limiter{
		entries: make(map[string]*entry),
		limit:   limit,
		window:  window,
	}
	go l.cleanup()
	return l
}

func (l *Limiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for k, e := range l.entries {
			e.timestamps = filterRecent(e.timestamps, now, l.window)
			if len(e.timestamps) == 0 {
				delete(l.entries, k)
			}
		}
		l.mu.Unlock()
	}
}

func (l *Limiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[ip]
	if !ok {
		e = &entry{}
		l.entries[ip] = e
	}
	e.timestamps = filterRecent(e.timestamps, now, l.window)
	if len(e.timestamps) >= l.limit {
		return false
	}
	e.timestamps = append(e.timestamps, now)
	return true
}

func (l *Limiter) Count(ip string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		return 0
	}
	e.timestamps = filterRecent(e.timestamps, time.Now(), l.window)
	return len(e.timestamps)
}

func (l *Limiter) Reset(ip string) {
	l.mu.Lock()
	delete(l.entries, ip)
	l.mu.Unlock()
}

func filterRecent(ts []time.Time, now time.Time, window time.Duration) []time.Time {
	cutoff := now.Add(-window)
	result := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			result = append(result, t)
		}
	}
	return result
}

func ExtractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i, c := range xff {
			if c == ',' {
				return xff[:i]
			}
			_ = i
		}
		return xff
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *Limiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ExtractIP(r)
			if !l.Allow(ip) {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
