package main

import (
	"context"
	"encoding/base64"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/veggiemonk/cloud-run-auth/internal/assets"
	"github.com/veggiemonk/cloud-run-auth/internal/handler/oauthhandler"
	"github.com/veggiemonk/cloud-run-auth/internal/middleware"
	"github.com/veggiemonk/cloud-run-auth/internal/session"
	"github.com/veggiemonk/cloud-run-auth/internal/shared"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/reqlog"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Required env vars.
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURL := os.Getenv("OAUTH_REDIRECT_URL")
	projectID := os.Getenv("PROJECT_ID")
	encKeyB64 := os.Getenv("SESSION_ENCRYPTION_KEY")

	if clientID == "" || clientSecret == "" {
		slog.Error("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set")
		os.Exit(1)
	}
	if projectID == "" {
		slog.Error("PROJECT_ID must be set")
		os.Exit(1)
	}
	if encKeyB64 == "" {
		slog.Error("SESSION_ENCRYPTION_KEY must be set (base64-encoded 32-byte key)")
		os.Exit(1)
	}

	encKey, err := base64.StdEncoding.DecodeString(encKeyB64)
	if err != nil || len(encKey) != 32 {
		slog.Error("SESSION_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
		os.Exit(1)
	}

	if redirectURL == "" {
		redirectURL = "http://localhost:8080/auth/callback"
		slog.Warn("OAUTH_REDIRECT_URL not set, using default", "url", redirectURL)
	}

	databaseID := os.Getenv("FIRESTORE_DATABASE")
	if databaseID == "" {
		databaseID = "(default)"
	}

	kRevision := os.Getenv("K_REVISION")
	csrfKeyB64 := os.Getenv("CSRF_KEY")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize Firestore session store with encryption.
	ctx := context.Background()
	store, err := session.NewStore(ctx, projectID, databaseID, encKey)
	if err != nil {
		slog.Error("failed to initialize session store", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// Create OAuth config (reuse existing package).
	cfg := newGoogleConfig(clientID, clientSecret, redirectURL)

	// Cookie config (production vs dev).
	cookies := NewCookieConfig(kRevision)

	// CSRF protection.
	csrf, err := middleware.NewCSRF(csrfKeyB64)
	if err != nil {
		slog.Error("failed to initialize CSRF", "error", err)
		os.Exit(1)
	}

	// Auth dependencies.
	deps := &authDeps{
		oauthCfg: cfg,
		store:    store,
		cookies:  cookies,
		csrf:     csrf,
	}

	// Rate limiters.
	authLimiter := middleware.NewIPRateLimiter(AuthRateLimitBurst, time.Minute)
	userLimiter := middleware.NewUserRateLimiter(UserRateLimitBurst, time.Minute, deps.emailFromSession)

	// Request log buffer.
	buf := reqlog.NewBuffer()

	// Routes.
	mux := http.NewServeMux()

	// Static files.
	staticFS, err := fs.Sub(assets.StaticFiles, "static")
	if err != nil {
		slog.Error("failed to create static sub-filesystem", "error", err)
		os.Exit(1)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Health check (no auth).
	mux.Handle("GET /healthz", oauthhandler.Healthz())

	// Auth routes (IP rate limited).
	mux.Handle("GET /auth/login", authLimiter.Limit(deps.prodLoginHandler()))
	mux.Handle("GET /auth/callback", authLimiter.Limit(deps.prodCallbackHandler()))

	// Logout (POST-only, requires auth + CSRF).
	logoutChain := deps.requireAuth(
		csrf.RequireCSRF(deps.sessionIDFromCookie)(
			deps.prodLogoutHandler(),
		),
	)
	mux.Handle("POST /auth/logout", logoutChain)

	// Protected routes (auth with refresh + user rate limit + CSRF).
	protected := http.NewServeMux()
	protected.Handle("GET /", oauthhandler.Dashboard())
	protected.Handle("GET /token", oauthhandler.Token())
	protected.Handle("GET /gcp", oauthhandler.GCPExplorer())
	protected.Handle("GET /diagnostic", oauthhandler.Diagnostic())

	protectedChain := deps.requireAuthWithRefresh(
		userLimiter.Limit(
			csrf.RequireCSRF(deps.sessionIDFromCookie)(
				protected,
			),
		),
	)
	mux.Handle("/", protectedChain)

	// Middleware chain (outermost first).
	handler := shared.LoggingMiddleware(logger,
		middleware.SecurityHeaders(
			middleware.MaxBodySize(MaxBodyBytes)(
				shared.RequestLogMiddleware(buf, deps.emailFromSession, "oauth", mux),
			),
		),
	)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadTimeout:       ReadTimeout,
		ReadHeaderTimeout: ReadHeaderTimeout,
		IdleTimeout:       IdleTimeout,
	}

	slog.Info("starting production OAuth server", "port", port, "secure", cookies.Secure)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// newGoogleConfig creates an OAuth2 config. We inline this rather than importing
// oauth.NewGoogleConfig to avoid pulling in the in-memory session store dependency.
func newGoogleConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
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
