package oauthhandler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/veggiemonk/cloud-run-auth/internal/components/oauthui"
	"github.com/veggiemonk/cloud-run-auth/internal/oauth"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/render"
)

// Dashboard returns a handler for the OAuth dashboard page.
func Dashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := oauth.UserFromContext(r.Context())
		if user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		data := oauthui.DashboardData{
			Email:   user.Email,
			Name:    user.Name,
			Picture: user.Picture,
		}

		if user.Token != nil && !user.Token.Expiry.IsZero() {
			data.TokenExpiry = user.Token.Expiry.Format(time.RFC3339)
		}

		// Session age is computed from cookie-based session; we show time since token was issued.
		if user.Token != nil && !user.Token.Expiry.IsZero() {
			// Approximate session age: tokens typically last 1 hour.
			age := time.Since(user.Token.Expiry.Add(-1 * time.Hour))
			if age > 0 {
				data.SessionAge = age.Round(time.Second).String()
			}
		}

		if render.WantsJSON(r) {
			render.JSON(w, data)
			return
		}

		if err := oauthui.DashboardPage(data).Render(r.Context(), w); err != nil {
			slog.Error("failed to render oauth dashboard", "error", err)
		}
	}
}
