package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/veggiemonk/cloud-run-auth/internal/middleware"
)

func TestIPRateLimiter_BurstEnforced(t *testing.T) {
	rl := middleware.NewIPRateLimiter(3, time.Minute)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.Limit(inner)

	for i := range 3 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i, rec.Code)
		}
	}

	// Next request should be rate limited.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request after burst: got %d, want 429", rec.Code)
	}
}

func TestIPRateLimiter_PerIPIsolation(t *testing.T) {
	rl := middleware.NewIPRateLimiter(1, time.Minute)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.Limit(inner)

	// Exhaust limit for IP A.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.1.1.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("IP A first request: got %d, want 200", rec.Code)
	}

	// IP B should still be allowed.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "2.2.2.2:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("IP B first request: got %d, want 200", rec.Code)
	}
}

func TestClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	if got := middleware.ClientIP(req); got != "10.0.0.1" {
		t.Errorf("ClientIP = %q, want %q", got, "10.0.0.1")
	}
}

func TestClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:4321"
	if got := middleware.ClientIP(req); got != "192.168.1.1:4321" {
		t.Errorf("ClientIP = %q, want %q", got, "192.168.1.1:4321")
	}
}
