package iaphandler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/veggiemonk/cloud-run-auth/internal/components/iapui"
	"github.com/veggiemonk/cloud-run-auth/internal/iap"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/render"
)

// Diagnostic returns a handler that runs IAP diagnostic checks.
func Diagnostic(verifier *iap.Verifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		det := iap.DetectionResultFromContext(r)
		var checks []iapui.Check

		// Check 1: JWT present?
		if det.HasJWT {
			checks = append(checks, iapui.Check{
				Name:   "JWT Present",
				Status: "pass",
				Detail: "X-Goog-IAP-JWT-Assertion header found",
			})
		} else {
			checks = append(checks, iapui.Check{
				Name:   "JWT Present",
				Status: "fail",
				Detail: "X-Goog-IAP-JWT-Assertion header not found — IAP may not be enabled",
			})
		}

		// Decode/verify JWT for remaining checks.
		if det.HasJWT {
			var result *iap.VerificationResult
			if verifier.ExpectedAudience() != "" {
				result = verifier.Verify(r.Context(), det.RawJWT)
			} else {
				result = verifier.Decode(det.RawJWT)
			}

			// Check 2: Signature valid?
			if result.Valid {
				checks = append(checks, iapui.Check{
					Name:   "Signature Valid",
					Status: "pass",
					Detail: "JWT signature verified with Google's public keys",
				})
			} else if verifier.ExpectedAudience() == "" {
				checks = append(checks, iapui.Check{
					Name:   "Signature Valid",
					Status: "warn",
					Detail: "Cannot verify signature: IAP_AUDIENCE not configured",
				})
			} else {
				checks = append(checks, iapui.Check{
					Name:   "Signature Valid",
					Status: "fail",
					Detail: result.Error,
				})
			}

			// Check 3: Issuer correct?
			if result.Claims != nil {
				if result.Claims.Issuer == "https://cloud.google.com/iap" {
					checks = append(checks, iapui.Check{
						Name:   "Issuer",
						Status: "pass",
						Detail: "Issuer is https://cloud.google.com/iap",
					})
				} else {
					checks = append(checks, iapui.Check{
						Name:   "Issuer",
						Status: "fail",
						Detail: "Expected issuer https://cloud.google.com/iap, got: " + result.Claims.Issuer,
					})
				}

				// Check 4: Not expired?
				if result.Claims.ExpiresAt.After(time.Now()) {
					checks = append(checks, iapui.Check{
						Name:   "Token Expiry",
						Status: "pass",
						Detail: "Token expires at " + result.Claims.ExpiresAt.Format(time.RFC3339),
					})
				} else if !result.Claims.ExpiresAt.IsZero() {
					checks = append(checks, iapui.Check{
						Name:   "Token Expiry",
						Status: "fail",
						Detail: "Token expired at " + result.Claims.ExpiresAt.Format(time.RFC3339),
					})
				}
			}

			// Check 5: Email header matches JWT?
			if det.HasEmailHeader && result.Claims != nil && result.Claims.Email != "" {
				if det.Email == result.Claims.Email {
					checks = append(checks, iapui.Check{
						Name:   "Email Consistency",
						Status: "pass",
						Detail: "Email header matches JWT claim: " + result.Claims.Email,
					})
				} else {
					checks = append(checks, iapui.Check{
						Name:   "Email Consistency",
						Status: "fail",
						Detail: "Email header (" + det.Email + ") does not match JWT claim (" + result.Claims.Email + ")",
					})
				}
			}
		}

		// Check 6: Bypass warning?
		if det.Warning != "" {
			checks = append(checks, iapui.Check{
				Name:   "Bypass Risk",
				Status: "warn",
				Detail: det.Warning,
			})
		}

		data := iapui.DiagnosticData{Checks: checks}

		if render.WantsJSON(r) {
			render.JSON(w, data)
			return
		}

		if err := iapui.DiagnosticPage(data).Render(r.Context(), w); err != nil {
			slog.Error("failed to render diagnostic", "error", err)
		}
	}
}
