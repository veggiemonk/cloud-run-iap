package oauthhandler

import (
	"log/slog"
	"net/http"

	"google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/option"

	"github.com/veggiemonk/cloud-run-auth/internal/components/oauthui"
	"github.com/veggiemonk/cloud-run-auth/internal/oauth"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/render"
)

// GCPExplorer returns a handler that lists the user's GCP projects.
func GCPExplorer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := oauth.UserFromContext(r.Context())
		if user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		data := oauthui.GCPData{}

		ts := user.OAuthConfig.TokenSource(r.Context(), user.Token)
		svc, err := cloudresourcemanager.NewService(r.Context(), option.WithTokenSource(ts))
		if err != nil {
			data.Error = "Failed to create GCP client: " + err.Error()
			slog.Error("failed to create cloudresourcemanager service", "error", err)
		} else {
			resp, err := svc.Projects.Search().Do()
			if err != nil {
				data.Error = "Failed to list projects: " + err.Error()
				slog.Error("failed to search projects", "error", err)
			} else {
				for _, p := range resp.Projects {
					data.Projects = append(data.Projects, oauthui.GCPProject{
						ID:   p.ProjectId,
						Name: p.DisplayName,
					})
				}
			}
		}

		if render.WantsJSON(r) {
			render.JSON(w, data)
			return
		}

		if err := oauthui.GCPPage(data).Render(r.Context(), w); err != nil {
			slog.Error("failed to render gcp page", "error", err)
		}
	}
}
