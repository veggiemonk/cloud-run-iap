package iaphandler

import (
	"log/slog"
	"net/http"

	"github.com/veggiemonk/cloud-run-auth/internal/components/iapui"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/render"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/reqlog"
)

// Log returns a handler that displays the request log.
func Log(buf *reqlog.Buffer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := iapui.LogData{
			Entries: buf.Entries(),
		}

		if render.WantsJSON(r) {
			render.JSON(w, data)
			return
		}

		if err := iapui.LogPage(data).Render(r.Context(), w); err != nil {
			slog.Error("failed to render log", "error", err)
		}
	}
}
