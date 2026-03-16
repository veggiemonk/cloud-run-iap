package oauth

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
)

type contextKey string

const userContextKey contextKey = "oauth_user"

// UserInfo holds the authenticated user's information.
type UserInfo struct {
	Email       string
	Name        string
	Picture     string
	Token       *oauth2.Token
	OAuthConfig *oauth2.Config
}

// RequireAuth is middleware that ensures the user has a valid session.
// If unauthenticated, it redirects to /auth/login.
func RequireAuth(sessions *SessionStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		session := sessions.Get(cookie.Value)
		if session == nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		user := &UserInfo{
			Email:       session.Email,
			Name:        session.Name,
			Picture:     session.Picture,
			Token:       session.Token,
			OAuthConfig: sessions.OAuthConfig,
		}

		ctx := WithUser(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// WithUser stores user info in the context.
func WithUser(ctx context.Context, user *UserInfo) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext retrieves user info from the context.
func UserFromContext(ctx context.Context) *UserInfo {
	user, _ := ctx.Value(userContextKey).(*UserInfo)
	return user
}
