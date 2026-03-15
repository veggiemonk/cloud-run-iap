package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/veggiemonk/cloud-run-auth/internal/middleware"
)

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.SecurityHeaders(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	tests := []struct {
		header, want string
	}{
		{"Content-Security-Policy", middleware.ContentSecurityPolicy},
		{"X-Content-Type-Options", middleware.XContentTypeOptionsValue},
		{"X-Frame-Options", middleware.XFrameOptionsValue},
		{"Referrer-Policy", middleware.ReferrerPolicyValue},
	}

	for _, tt := range tests {
		if got := rec.Header().Get(tt.header); got != tt.want {
			t.Errorf("header %s = %q, want %q", tt.header, got, tt.want)
		}
	}
}
