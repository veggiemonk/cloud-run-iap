package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
)

const (
	csrfKeyMinBytes   = 32
	CSRFFormFieldName = "csrf_token"
	CSRFHeaderName    = "X-CSRF-Token"
)

// CSRF provides HMAC-SHA256 CSRF token generation and validation.
type CSRF struct {
	key []byte
}

// NewCSRF creates a CSRF instance. If encodedKey is non-empty, it is decoded
// from base64 and must be at least 32 bytes. If empty, a random key is generated
// (acceptable for single-instance / local dev).
func NewCSRF(encodedKey string) (*CSRF, error) {
	if encodedKey != "" {
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		if err != nil || len(key) < csrfKeyMinBytes {
			return nil, errors.New("CSRF_KEY must be a base64-encoded key of at least 32 bytes")
		}
		return &CSRF{key: key}, nil
	}

	key := make([]byte, csrfKeyMinBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating CSRF key: %w", err)
	}
	return &CSRF{key: key}, nil
}

// Token derives a CSRF token from a session ID using HMAC-SHA256.
func (c *CSRF) Token(sessionID string) string {
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ValidToken checks whether the provided token matches the expected
// CSRF token for the given session.
func (c *CSRF) ValidToken(sessionID, token string) bool {
	expected := c.Token(sessionID)
	return hmac.Equal([]byte(expected), []byte(token))
}

// RequireCSRF validates CSRF tokens on state-changing requests (POST, PUT, DELETE, PATCH).
// sessionIDFromRequest extracts the session ID from the request (e.g., from a cookie).
func (c *CSRF) RequireCSRF(sessionIDFromRequest func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
				sessionID := sessionIDFromRequest(r)
				if sessionID == "" {
					next.ServeHTTP(w, r)
					return
				}
				token := r.FormValue(CSRFFormFieldName)
				if token == "" {
					token = r.Header.Get(CSRFHeaderName)
				}
				if !c.ValidToken(sessionID, token) {
					http.Error(w, "Forbidden — invalid CSRF token", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
