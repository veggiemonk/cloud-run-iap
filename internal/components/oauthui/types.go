package oauthui

import shared "github.com/veggiemonk/cloud-run-auth/internal/shared/components"

// DashboardData holds the data for the OAuth dashboard view.
type DashboardData struct {
	Email       string `json:"email,omitempty"`
	Name        string `json:"name,omitempty"`
	Picture     string `json:"picture,omitempty"`
	TokenExpiry string `json:"token_expiry,omitempty"`
	SessionAge  string `json:"session_age,omitempty"`
}

// TokenData holds the data for the token inspection view.
type TokenData struct {
	AccessTokenMasked string   `json:"access_token_masked,omitempty"`
	HasRefreshToken   bool     `json:"has_refresh_token"`
	Scopes            []string `json:"scopes,omitempty"`
	Expiry            string   `json:"expiry,omitempty"`
	TokenType         string   `json:"token_type,omitempty"`
}

// GCPProject represents a Google Cloud project.
type GCPProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GCPData holds the data for the GCP explorer view.
type GCPData struct {
	Projects []GCPProject `json:"projects,omitempty"`
	Error    string       `json:"error,omitempty"`
}

// Check represents a single diagnostic check result.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pass", "fail", "warn"
	Detail string `json:"detail"`
}

// DiagnosticData holds the data for the diagnostic view.
type DiagnosticData struct {
	Checks []Check `json:"checks"`
}

// OAuthNav returns the NavConfig for the OAuth application.
func OAuthNav() shared.NavConfig {
	return shared.NavConfig{
		Brand: "RunOAuth",
		Items: []shared.NavItem{
			{Href: "/", Label: "Dashboard", Page: "dashboard"},
			{Href: "/token", Label: "Token", Page: "token"},
			{Href: "/gcp", Label: "GCP Explorer", Page: "gcp"},
			{Href: "/diagnostic", Label: "Diagnostic", Page: "diagnostic"},
		},
	}
}
