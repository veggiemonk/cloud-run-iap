package iaphandler

import (
	"log/slog"
	"net/http"

	"github.com/veggiemonk/cloud-run-auth/internal/components/iapui"
	"github.com/veggiemonk/cloud-run-auth/internal/iap"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/render"
)

// Audience returns a handler for audience validation (GET + POST).
func Audience(verifier *iap.Verifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := iapui.AudienceData{
			FormatHelp: "/projects/PROJECT_NUMBER/locations/REGION/services/SERVICE_NAME",
		}

		// Extract current audience from JWT if present.
		det := iap.DetectionResultFromContext(r)
		if det.HasJWT {
			result := verifier.Decode(det.RawJWT)
			if result.Claims != nil {
				data.CurrentAudience = result.Claims.Audience
			}
		}

		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Bad request: invalid form data", http.StatusBadRequest)
				return
			}
			expected := r.FormValue("audience")
			data.ExpectedAudience = expected
			data.Checked = true
			data.Match = expected != "" && expected == data.CurrentAudience
		} else {
			data.ExpectedAudience = verifier.ExpectedAudience()
		}

		if render.WantsJSON(r) {
			render.JSON(w, data)
			return
		}

		if err := iapui.AudiencePage(data).Render(r.Context(), w); err != nil {
			slog.Error("failed to render audience", "error", err)
		}
	}
}
