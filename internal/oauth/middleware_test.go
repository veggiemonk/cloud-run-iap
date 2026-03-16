package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
)

func TestRequireAuth_NoCookie(t *testing.T) {
	store := NewSessionStore(nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})

	handler := RequireAuth(store, next)
	req := httptest.NewRequest("GET", "/protected", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/auth/login" {
		t.Errorf("expected redirect to /auth/login, got %s", loc)
	}
}

func TestRequireAuth_InvalidSession(t *testing.T) {
	store := NewSessionStore(nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})

	handler := RequireAuth(store, next)
	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "nonexistent"})
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rr.Code)
	}
}

func TestRequireAuth_ValidSession(t *testing.T) {
	store := NewSessionStore(nil)
	token := &oauth2.Token{AccessToken: "test-token"}
	session := store.Create("user@example.com", "Test User", "pic.jpg", token)

	var gotUser *UserInfo
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(store, next)
	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: session.ID})
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotUser == nil {
		t.Fatal("expected user in context")
	}
	if gotUser.Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %s", gotUser.Email)
	}
	if gotUser.Name != "Test User" {
		t.Errorf("expected name Test User, got %s", gotUser.Name)
	}
}

func TestUserFromContext_NilWhenMissing(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	user := UserFromContext(req.Context())
	if user != nil {
		t.Error("expected nil user from empty context")
	}
}
