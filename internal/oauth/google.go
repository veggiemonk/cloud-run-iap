package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// NewGoogleConfig creates an OAuth2 config for Google authentication.
func NewGoogleConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			"openid",
			"email",
			"profile",
			"https://www.googleapis.com/auth/cloud-platform.read-only",
		},
		Endpoint: google.Endpoint,
	}
}

// LoginHandler generates a state param, stores it in a cookie, and redirects to Google auth.
func LoginHandler(cfg *oauth2.Config, sessions *SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := generateState()

		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    state,
			Path:     "/",
			MaxAge:   600, // 10 minutes
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
		http.Redirect(w, r, url, http.StatusFound)
	}
}

// userInfo holds the response from Google's userinfo endpoint.
type userInfo struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// CallbackHandler validates the state cookie, exchanges the code for a token,
// fetches user info, creates a session, and redirects to /.
func CallbackHandler(cfg *oauth2.Config, sessions *SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Validate state.
		stateCookie, err := r.Cookie("oauth_state")
		if err != nil {
			http.Error(w, "Missing state cookie", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != stateCookie.Value {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			return
		}

		// Clear state cookie.
		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
		})

		// Check for error from OAuth provider.
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			http.Error(w, "OAuth error: "+errParam, http.StatusBadRequest)
			return
		}

		// Exchange code for token.
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}

		token, err := cfg.Exchange(r.Context(), code)
		if err != nil {
			slog.Error("token exchange failed", "error", err)
			http.Error(w, "Token exchange failed", http.StatusInternalServerError)
			return
		}

		// Fetch user info.
		info, err := fetchUserInfo(r.Context(), cfg, token)
		if err != nil {
			slog.Error("failed to fetch user info", "error", err)
			http.Error(w, "Failed to fetch user info", http.StatusInternalServerError)
			return
		}

		// Create session.
		session := sessions.Create(info.Email, info.Name, info.Picture, token)

		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// LogoutHandler deletes the session, clears the cookie, and redirects to login.
func LogoutHandler(sessions *SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("session_id"); err == nil {
			sessions.Delete(cookie.Value)
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
		})

		http.Redirect(w, r, "/auth/login", http.StatusFound)
	}
}

// fetchUserInfo retrieves the user's profile from Google's userinfo endpoint.
func fetchUserInfo(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (*userInfo, error) {
	client := cfg.Client(ctx, token)
	client.Timeout = 10 * time.Second

	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}

	var info userInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode userinfo: %w", err)
	}
	return &info, nil
}

// generateState generates a 32-byte random hex-encoded state parameter.
func generateState() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
