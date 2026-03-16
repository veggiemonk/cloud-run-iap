package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/veggiemonk/cloud-run-auth/internal/middleware"
)

func TestCSRF_TokenGeneration(t *testing.T) {
	csrf, err := middleware.NewCSRF("")
	if err != nil {
		t.Fatal(err)
	}

	token1 := csrf.Token("session-1")
	token2 := csrf.Token("session-1")
	if token1 != token2 {
		t.Error("same session should produce same token")
	}

	token3 := csrf.Token("session-2")
	if token1 == token3 {
		t.Error("different sessions should produce different tokens")
	}
}

func TestCSRF_ValidToken(t *testing.T) {
	csrf, err := middleware.NewCSRF("")
	if err != nil {
		t.Fatal(err)
	}

	token := csrf.Token("sess-abc")
	if !csrf.ValidToken("sess-abc", token) {
		t.Error("valid token rejected")
	}
	if csrf.ValidToken("sess-abc", "wrong-token") {
		t.Error("invalid token accepted")
	}
}

func TestCSRF_RequireCSRF_BlocksWithoutToken(t *testing.T) {
	csrf, err := middleware.NewCSRF("")
	if err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	getSessionID := func(r *http.Request) string {
		if c, err := r.Cookie("session"); err == nil {
			return c.Value
		}
		return ""
	}

	handler := csrf.RequireCSRF(getSessionID)(inner)

	// POST without CSRF token should be rejected.
	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: "sess-123"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF: got %d, want 403", rec.Code)
	}
}

func TestCSRF_RequireCSRF_AllowsValidToken(t *testing.T) {
	csrf, err := middleware.NewCSRF("")
	if err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	getSessionID := func(r *http.Request) string {
		if c, err := r.Cookie("session"); err == nil {
			return c.Value
		}
		return ""
	}

	handler := csrf.RequireCSRF(getSessionID)(inner)
	token := csrf.Token("sess-123")

	form := url.Values{middleware.CSRFFormFieldName: {token}}
	req := httptest.NewRequest(http.MethodPost, "/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: "sess-123"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST with valid CSRF: got %d, want 200", rec.Code)
	}
}

func TestCSRF_RequireCSRF_AllowsGET(t *testing.T) {
	csrf, err := middleware.NewCSRF("")
	if err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	getSessionID := func(r *http.Request) string { return "sess-123" }
	handler := csrf.RequireCSRF(getSessionID)(inner)

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET should pass through: got %d, want 200", rec.Code)
	}
}
