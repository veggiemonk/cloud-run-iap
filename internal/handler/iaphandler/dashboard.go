package iaphandler

import (
	"log/slog"
	"net/http"

	"github.com/veggiemonk/cloud-run-auth/internal/components/iapui"
	"github.com/veggiemonk/cloud-run-auth/internal/iap"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/render"
)

// Dashboard returns a handler for the main dashboard page.
func Dashboard(verifier *iap.Verifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		det := iap.DetectionResultFromContext(r)

		data := iapui.DashboardData{
			HasIAP:     det.HasJWT,
			IAPWarning: det.Warning,
		}

		if det.HasJWT {
			var result *iap.VerificationResult
			if verifier.ExpectedAudience() != "" {
				result = verifier.Verify(r.Context(), det.RawJWT)
			} else {
				result = verifier.Decode(det.RawJWT)
			}
			data.JWTValid = result.Valid
			data.JWTError = result.Error
			if result.Valid && result.Claims != nil {
				// Prefer verified JWT claims over unsigned headers.
				data.Email = result.Claims.Email
				data.HostedDomain = result.Claims.HostedDomain
				data.UserID = result.Claims.Subject
			} else {
				// Fall back to unsigned headers only if JWT isn't verified.
				data.Email = det.Email
				data.UserID = det.UserID
			}
		} else {
			data.Email = det.Email
			data.UserID = det.UserID
		}

		if render.WantsJSON(r) {
			render.JSON(w, data)
			return
		}

		if err := iapui.DashboardPage(data).Render(r.Context(), w); err != nil {
			slog.Error("failed to render dashboard", "error", err)
		}
	}
}
