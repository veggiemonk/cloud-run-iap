package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/veggiemonk/cloud-run-auth/internal/middleware"
	"github.com/veggiemonk/cloud-run-auth/internal/oauth"
	"github.com/veggiemonk/cloud-run-auth/internal/session"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
)

// userInfoResponse holds the response from Google's userinfo endpoint.
type userInfoResponse struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	HD      string `json:"hd"`
}

// authDeps holds shared dependencies for auth handlers.
type authDeps struct {
	oauthCfg      *oauth2.Config
	store         *session.Store
	cookies       CookieConfig
	csrf          *middleware.CSRF
	allowedDomain string
	sfGroup       singleflight.Group
}

// prodLoginHandler redirects to Google OAuth with domain hint.
func (d *authDeps) prodLoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := generateState()

		http.SetCookie(w, d.cookies.NewCookie(d.cookies.OAuthStateName, state, OAuthStateCookieMaxAge))

		url := d.oauthCfg.AuthCodeURL(state,
			oauth2.AccessTypeOffline,
			oauth2.SetAuthURLParam("hd", d.allowedDomain),
		)
		http.Redirect(w, r, url, http.StatusFound)
	}
}

// prodCallbackHandler handles the OAuth callback with production security checks.
func (d *authDeps) prodCallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Validate state cookie.
		stateCookie, err := r.Cookie(d.cookies.OAuthStateName)
		if err != nil {
			http.Error(w, "Missing state cookie", http.StatusBadRequest)
			return
		}

		// Clear state cookie immediately.
		http.SetCookie(w, d.cookies.DeleteCookie(d.cookies.OAuthStateName))

		// Constant-time state comparison.
		stateParam := r.URL.Query().Get("state")
		if subtle.ConstantTimeCompare([]byte(stateParam), []byte(stateCookie.Value)) != 1 {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			return
		}

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

		token, err := d.oauthCfg.Exchange(r.Context(), code)
		if err != nil {
			slog.Error("token exchange failed", "error", err)
			http.Error(w, "Token exchange failed", http.StatusInternalServerError)
			return
		}

		// Fetch user info.
		info, err := fetchUserInfoProd(r.Context(), d.oauthCfg, token)
		if err != nil {
			slog.Error("failed to fetch user info", "error", err)
			http.Error(w, "Failed to fetch user info", http.StatusInternalServerError)
			return
		}

		// Verify domain.
		if info.HD != d.allowedDomain {
			slog.Warn("domain mismatch", "email", info.Email, "hd", info.HD)
			http.Error(w, "Access restricted to "+d.allowedDomain+" accounts", http.StatusForbidden)
			return
		}

		// Validate picture URL is HTTPS (or empty).
		if info.Picture != "" && !strings.HasPrefix(info.Picture, "https://") {
			info.Picture = ""
		}

		// Create Firestore session with encrypted tokens.
		sessionID, err := d.store.Create(r.Context(), info.Email, info.Name, info.Picture, token)
		if err != nil {
			slog.Error("failed to create session", "error", err)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, d.cookies.NewCookie(d.cookies.SessionName, sessionID, SessionCookieMaxAge))
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// prodLogoutHandler handles POST-only logout.
func (d *authDeps) prodLogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if cookie, err := r.Cookie(d.cookies.SessionName); err == nil {
			d.store.Delete(r.Context(), cookie.Value)
		}

		http.SetCookie(w, d.cookies.DeleteCookie(d.cookies.SessionName))
		http.Redirect(w, r, "/auth/login", http.StatusFound)
	}
}

// requireAuth checks for a valid session without token refresh.
func (d *authDeps) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(d.cookies.SessionName)
		if err != nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		sess, err := d.store.Get(r.Context(), cookie.Value)
		if err != nil || sess == nil {
			http.SetCookie(w, d.cookies.DeleteCookie(d.cookies.SessionName))
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		user := &oauth.UserInfo{
			Email:       sess.Email,
			Name:        sess.Name,
			Picture:     sess.Picture,
			Token:       sess.Token(),
			OAuthConfig: d.oauthCfg,
		}

		ctx := oauth.WithUser(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuthWithRefresh checks for a valid session and refreshes token if near expiry.
func (d *authDeps) requireAuthWithRefresh(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(d.cookies.SessionName)
		if err != nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		sessionID := cookie.Value
		sess, err := d.store.Get(r.Context(), sessionID)
		if err != nil || sess == nil {
			http.SetCookie(w, d.cookies.DeleteCookie(d.cookies.SessionName))
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		tok := sess.Token()

		// Refresh token if near expiry, using singleflight for dedup.
		if time.Until(tok.Expiry) < TokenRefreshThreshold && tok.RefreshToken != "" {
			refreshed, err, _ := d.sfGroup.Do(sessionID, func() (any, error) {
				src := d.oauthCfg.TokenSource(r.Context(), tok)
				newTok, err := src.Token()
				if err != nil {
					return nil, err
				}
				if newTok.AccessToken != tok.AccessToken {
					if updateErr := d.store.UpdateToken(r.Context(), sessionID, newTok); updateErr != nil {
						slog.Error("failed to update token", "error", updateErr)
					}
				}
				return newTok, nil
			})
			if err == nil {
				tok = refreshed.(*oauth2.Token)
			} else {
				slog.Warn("token refresh failed", "error", err, "session", sessionID)
			}
		}

		user := &oauth.UserInfo{
			Email:       sess.Email,
			Name:        sess.Name,
			Picture:     sess.Picture,
			Token:       tok,
			OAuthConfig: d.oauthCfg,
		}

		ctx := oauth.WithUser(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sessionIDFromCookie returns a function that extracts session ID from the cookie.
func (d *authDeps) sessionIDFromCookie(r *http.Request) string {
	if c, err := r.Cookie(d.cookies.SessionName); err == nil {
		return c.Value
	}
	return ""
}

// emailFromSession returns the email for the current session (for rate limiting/logging).
func (d *authDeps) emailFromSession(r *http.Request) string {
	user := oauth.UserFromContext(r.Context())
	if user != nil {
		return user.Email
	}
	return ""
}

// fetchUserInfoProd retrieves user profile from Google's userinfo endpoint.
func fetchUserInfoProd(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (*userInfoResponse, error) {
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

	var info userInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode userinfo: %w", err)
	}
	return &info, nil
}

func generateState() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
