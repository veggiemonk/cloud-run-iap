package oauthhandler

import (
	"log/slog"
	"net/http"
	"time"

	"google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/option"

	"github.com/veggiemonk/cloud-run-auth/internal/components/oauthui"
	"github.com/veggiemonk/cloud-run-auth/internal/oauth"
	"github.com/veggiemonk/cloud-run-auth/internal/shared/render"
)

// Diagnostic returns a handler that runs OAuth diagnostic checks.
func Diagnostic() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := oauth.UserFromContext(r.Context())
		if user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var checks []oauthui.Check

		// Check 1: Session valid?
		if user.Email != "" {
			checks = append(checks, oauthui.Check{
				Name:   "Session Valid",
				Status: "pass",
				Detail: "Authenticated as " + user.Email,
			})
		} else {
			checks = append(checks, oauthui.Check{
				Name:   "Session Valid",
				Status: "fail",
				Detail: "No user email in session",
			})
		}

		// Check 2: Token not expired?
		if user.Token != nil {
			if user.Token.Expiry.IsZero() {
				checks = append(checks, oauthui.Check{
					Name:   "Token Expiry",
					Status: "warn",
					Detail: "Token has no expiry set",
				})
			} else if user.Token.Expiry.After(time.Now()) {
				checks = append(checks, oauthui.Check{
					Name:   "Token Expiry",
					Status: "pass",
					Detail: "Token expires at " + user.Token.Expiry.Format(time.RFC3339),
				})
			} else {
				checks = append(checks, oauthui.Check{
					Name:   "Token Expiry",
					Status: "fail",
					Detail: "Token expired at " + user.Token.Expiry.Format(time.RFC3339),
				})
			}
		} else {
			checks = append(checks, oauthui.Check{
				Name:   "Token Expiry",
				Status: "fail",
				Detail: "No token available",
			})
		}

		// Check 3: Scopes correct?
		if user.Token != nil {
			if extra := user.Token.Extra("scope"); extra != nil {
				if scopeStr, ok := extra.(string); ok && scopeStr != "" {
					checks = append(checks, oauthui.Check{
						Name:   "Scopes",
						Status: "pass",
						Detail: "Scopes: " + scopeStr,
					})
				} else {
					checks = append(checks, oauthui.Check{
						Name:   "Scopes",
						Status: "warn",
						Detail: "Scope information not available in token response",
					})
				}
			} else {
				checks = append(checks, oauthui.Check{
					Name:   "Scopes",
					Status: "warn",
					Detail: "Scope information not available in token response",
				})
			}
		}

		// Check 4: GCP API reachable?
		if user.Token != nil {
			ts := user.OAuthConfig.TokenSource(r.Context(), user.Token)
			svc, err := cloudresourcemanager.NewService(r.Context(), option.WithTokenSource(ts))
			if err != nil {
				checks = append(checks, oauthui.Check{
					Name:   "GCP API Reachable",
					Status: "fail",
					Detail: "Failed to create GCP client: " + err.Error(),
				})
			} else {
				_, err := svc.Projects.Search().Do()
				if err != nil {
					checks = append(checks, oauthui.Check{
						Name:   "GCP API Reachable",
						Status: "fail",
						Detail: "GCP API call failed: " + err.Error(),
					})
				} else {
					checks = append(checks, oauthui.Check{
						Name:   "GCP API Reachable",
						Status: "pass",
						Detail: "Successfully queried Cloud Resource Manager API",
					})
				}
			}
		} else {
			checks = append(checks, oauthui.Check{
				Name:   "GCP API Reachable",
				Status: "fail",
				Detail: "No token available to test GCP API",
			})
		}

		data := oauthui.DiagnosticData{Checks: checks}

		if render.WantsJSON(r) {
			render.JSON(w, data)
			return
		}

		if err := oauthui.DiagnosticPage(data).Render(r.Context(), w); err != nil {
			slog.Error("failed to render diagnostic", "error", err)
		}
	}
}
