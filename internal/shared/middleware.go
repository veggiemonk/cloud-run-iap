package shared

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/veggiemonk/cloud-run-auth/internal/shared/reqlog"
)

// StatusWriter wraps http.ResponseWriter to capture the status code.
type StatusWriter struct {
	http.ResponseWriter
	Status int
}

func (sw *StatusWriter) WriteHeader(code int) {
	sw.Status = code
	sw.ResponseWriter.WriteHeader(code)
}

// LoggingMiddleware logs each request using structured logging.
func LoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &StatusWriter{ResponseWriter: w, Status: 200}

		next.ServeHTTP(sw, r)

		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.Status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// RequestLogMiddleware records each request into the ring buffer.
// emailExtractor extracts the email from the request (app-specific).
// authType is the authentication type string (e.g., "iap", "oauth", "").
func RequestLogMiddleware(buf *reqlog.Buffer, emailExtractor func(*http.Request) string, authType string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		email := ""
		if emailExtractor != nil {
			email = emailExtractor(r)
		}

		buf.Add(reqlog.Entry{
			Timestamp:  time.Now(),
			Method:     r.Method,
			Path:       r.URL.Path,
			Email:      email,
			AuthType:   authType,
			RemoteAddr: r.RemoteAddr,
		})
	})
}
