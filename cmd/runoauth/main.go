package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/veggiemonk/cloud-run-auth/internal/assets"
	"github.com/veggiemonk/cloud-run-auth/internal/handler/oauthhandler"
	"github.com/veggiemonk/cloud-run-auth/internal/oauth"
	"github.com/veggiemonk/cloud-run-auth/internal/shared"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/reqlog"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURL := os.Getenv("OAUTH_REDIRECT_URL")

	if clientID == "" || clientSecret == "" {
		slog.Error("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set")
		os.Exit(1)
	}

	if redirectURL == "" {
		redirectURL = "http://localhost:8080/auth/callback"
		slog.Warn("OAUTH_REDIRECT_URL not set, using default", "url", redirectURL)
	}

	cfg := oauth.NewGoogleConfig(clientID, clientSecret, redirectURL)
	sessions := oauth.NewSessionStore(cfg)
	sessions.StartCleanup(5 * time.Minute)
	buf := reqlog.NewBuffer()

	mux := http.NewServeMux()

	// Static files.
	staticFS, err := fs.Sub(assets.StaticFiles, "static")
	if err != nil {
		slog.Error("failed to create static sub-filesystem", "error", err)
		os.Exit(1)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Health check (no auth required).
	mux.Handle("GET /healthz", oauthhandler.Healthz())

	// Auth routes (public).
	mux.Handle("GET /auth/login", oauth.LoginHandler(cfg, sessions))
	mux.Handle("GET /auth/callback", oauth.CallbackHandler(cfg, sessions))
	mux.Handle("GET /auth/logout", oauth.LogoutHandler(sessions))

	// Protected routes.
	protected := http.NewServeMux()
	protected.Handle("GET /", oauthhandler.Dashboard())
	protected.Handle("GET /token", oauthhandler.Token())
	protected.Handle("GET /gcp", oauthhandler.GCPExplorer())
	protected.Handle("GET /diagnostic", oauthhandler.Diagnostic())
	mux.Handle("/", oauth.RequireAuth(sessions, protected))

	// OAuth-specific email extractor reads directly from session store via cookie,
	// so it works regardless of middleware ordering.
	oauthEmailExtractor := func(r *http.Request) string {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			return ""
		}
		session := sessions.Get(cookie.Value)
		if session == nil {
			return ""
		}
		return session.Email
	}

	wrapped := shared.LoggingMiddleware(logger, shared.RequestLogMiddleware(buf, oauthEmailExtractor, "oauth", mux))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slog.Info("starting server", "port", port)
	if err := http.ListenAndServe(":"+port, wrapped); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
