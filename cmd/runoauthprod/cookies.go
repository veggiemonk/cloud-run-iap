package main

import "net/http"

// CookieConfig holds environment-aware cookie settings.
type CookieConfig struct {
	Secure         bool
	SessionName    string
	OAuthStateName string
}

// NewCookieConfig creates a CookieConfig based on whether the app is running
// on Cloud Run (kRevision non-empty) or locally.
func NewCookieConfig(kRevision string) CookieConfig {
	secure := kRevision != ""
	cc := CookieConfig{Secure: secure}
	if secure {
		cc.SessionName = "__Host-session"
		cc.OAuthStateName = "__Host-oauth-state"
	} else {
		cc.SessionName = "session"
		cc.OAuthStateName = "oauth-state"
	}
	return cc
}

// NewCookie creates an http.Cookie with security settings appropriate for the environment.
func (cc CookieConfig) NewCookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   cc.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

// DeleteCookie returns a cookie that clears the named cookie.
func (cc CookieConfig) DeleteCookie(name string) *http.Cookie {
	return cc.NewCookie(name, "", -1)
}
