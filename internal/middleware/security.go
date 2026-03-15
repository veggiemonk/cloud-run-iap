// Package middleware provides HTTP middleware for security headers, rate limiting, and body limits.
package middleware

import "net/http"

// Security header values.
const (
	ContentSecurityPolicy = "default-src 'self'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: https://*.googleusercontent.com; " +
		"script-src 'self'; " +
		"frame-ancestors 'none'"

	XContentTypeOptionsValue = "nosniff"
	XFrameOptionsValue       = "DENY"
	ReferrerPolicyValue      = "strict-origin-when-cross-origin"
)

// SecurityHeaders adds security headers to all responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", ContentSecurityPolicy)
		h.Set("X-Content-Type-Options", XContentTypeOptionsValue)
		h.Set("X-Frame-Options", XFrameOptionsValue)
		h.Set("Referrer-Policy", ReferrerPolicyValue)
		next.ServeHTTP(w, r)
	})
}
