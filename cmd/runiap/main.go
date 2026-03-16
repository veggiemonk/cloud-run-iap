package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/veggiemonk/cloud-run-auth/internal/assets"
	"github.com/veggiemonk/cloud-run-auth/internal/handler/iaphandler"
	"github.com/veggiemonk/cloud-run-auth/internal/iap"
	"github.com/veggiemonk/cloud-run-auth/internal/shared"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/reqlog"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	verifier := iap.NewVerifier()

	// Warn at startup if IAP_AUDIENCE is not configured on Cloud Run.
	if verifier.ExpectedAudience() == "" {
		if os.Getenv("K_SERVICE") != "" {
			slog.Error("IAP_AUDIENCE environment variable is not set — JWT verification is disabled. Set IAP_AUDIENCE to enable signature verification.")
		} else {
			slog.Warn("IAP_AUDIENCE not set — running in local/dev mode, JWT verification disabled")
		}
	}

	buf := reqlog.NewBuffer()

	mux := http.NewServeMux()

	// Static files (no auth required).
	staticFS, err := fs.Sub(assets.StaticFiles, "static")
	if err != nil {
		slog.Error("failed to create static sub-filesystem", "error", err)
		os.Exit(1)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Health check (no auth required).
	mux.Handle("GET /healthz", iaphandler.Healthz())

	// Protected routes — wrapped with IAP auth middleware.
	protected := http.NewServeMux()
	protected.Handle("GET /", iaphandler.Dashboard(verifier))
	protected.Handle("GET /headers", iaphandler.Headers())
	protected.Handle("GET /jwt", iaphandler.JWT(verifier))
	protected.Handle("GET /audience", iaphandler.Audience(verifier))
	protected.Handle("POST /audience", iaphandler.Audience(verifier))
	protected.Handle("GET /log", iaphandler.Log(buf))
	protected.Handle("GET /diagnostic", iaphandler.Diagnostic(verifier))
	mux.Handle("/", requireIAP(verifier, protected))

	// IAP-specific email extractor for request log middleware.
	iapEmailExtractor := func(r *http.Request) string {
		det := iap.Detect(r)
		return det.Email
	}

	// Wrap with middleware.
	wrapped := shared.LoggingMiddleware(logger, shared.RequestLogMiddleware(buf, iapEmailExtractor, "iap", mux))

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

// requireIAP rejects requests that don't have a valid IAP JWT.
// When IAP_AUDIENCE is configured, the JWT signature is verified.
// When running locally (no IAP_AUDIENCE), only the presence of the JWT header is checked.
func requireIAP(verifier *iap.Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		det := iap.Detect(r)
		if !det.HasJWT {
			http.Error(w, "Unauthorized: no IAP JWT present", http.StatusUnauthorized)
			return
		}

		if verifier.ExpectedAudience() != "" {
			result := verifier.Verify(r.Context(), det.RawJWT)
			if !result.Valid {
				http.Error(w, "Forbidden: invalid IAP JWT", http.StatusForbidden)
				return
			}
		}

		// Store detection result in context so handlers don't re-detect.
		next.ServeHTTP(w, iap.WithDetectionResult(r, det))
	})
}
