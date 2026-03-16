package render

import (
	"encoding/json"
	"net/http"
	"strings"
)

// WantsJSON returns true if the request prefers a JSON response,
// either via ?format=json query parameter or Accept header.
func WantsJSON(r *http.Request) bool {
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

// JSON writes data as a JSON response with proper content-type.
func JSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		http.Error(w, `{"error":"json encoding failed"}`, http.StatusInternalServerError)
	}
}
