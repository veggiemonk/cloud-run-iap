package iap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/api/idtoken"
)

// Verifier handles IAP JWT verification.
type Verifier struct {
	expectedAudience string
}

// NewVerifier creates a new Verifier, reading IAP_AUDIENCE from the environment.
func NewVerifier() *Verifier {
	return &Verifier{
		expectedAudience: os.Getenv("IAP_AUDIENCE"),
	}
}

// ExpectedAudience returns the configured expected audience string.
func (v *Verifier) ExpectedAudience() string {
	return v.expectedAudience
}

// VerificationResult holds the outcome of JWT verification or decoding.
type VerificationResult struct {
	Valid        bool           `json:"valid"`
	Error        string         `json:"error,omitempty"`
	Header       map[string]any `json:"header,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
	Claims       *Claims        `json:"claims,omitempty"`
	SignatureB64 string         `json:"signature_b64,omitempty"`
}

// Verify validates the JWT signature using Google's public keys via idtoken.Validate,
// and also decodes the header and payload for display.
func (v *Verifier) Verify(ctx context.Context, rawJWT string) *VerificationResult {
	result := v.Decode(rawJWT)
	if result.Error != "" {
		return result
	}

	if v.expectedAudience == "" {
		result.Error = "no IAP_AUDIENCE configured — cannot verify signature"
		return result
	}

	payload, err := idtoken.Validate(ctx, rawJWT, v.expectedAudience)
	if err != nil {
		result.Valid = false
		result.Error = fmt.Sprintf("signature verification failed: %v", err)
		return result
	}

	result.Valid = true
	// Overlay verified payload claims.
	result.Payload = payload.Claims
	result.Claims = parseClaims(payload.Claims)
	return result
}

// Decode decodes (but does not verify) a JWT, splitting it into header, payload,
// and signature for display purposes.
func (v *Verifier) Decode(rawJWT string) *VerificationResult {
	result := &VerificationResult{}

	parts := strings.SplitN(rawJWT, ".", 3)
	if len(parts) != 3 {
		result.Error = "invalid JWT format: expected 3 dot-separated parts"
		return result
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		result.Error = fmt.Sprintf("failed to decode JWT header: %v", err)
		return result
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		result.Error = fmt.Sprintf("failed to decode JWT payload: %v", err)
		return result
	}

	result.SignatureB64 = parts[2]

	var header map[string]any
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		result.Error = fmt.Sprintf("failed to parse JWT header JSON: %v", err)
		return result
	}
	result.Header = header

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		result.Error = fmt.Sprintf("failed to parse JWT payload JSON: %v", err)
		return result
	}
	result.Payload = payload
	result.Claims = parseClaims(payload)

	return result
}

// parseClaims extracts known IAP claims from a JWT payload map.
func parseClaims(payload map[string]any) *Claims {
	c := &Claims{}

	if v, ok := payload["iss"].(string); ok {
		c.Issuer = v
	}
	if v, ok := payload["sub"].(string); ok {
		c.Subject = v
	}
	if v, ok := payload["email"].(string); ok {
		c.Email = v
	}
	if v, ok := payload["hd"].(string); ok {
		c.HostedDomain = v
	}
	if v, ok := payload["aud"].(string); ok {
		c.Audience = v
	}
	if v, ok := payload["iat"].(float64); ok {
		c.IssuedAt = time.Unix(int64(v), 0)
	}
	if v, ok := payload["exp"].(float64); ok {
		c.ExpiresAt = time.Unix(int64(v), 0)
	}
	if g, ok := payload["google"].(map[string]any); ok {
		if v, ok := g["access_levels"].([]any); ok {
			for _, al := range v {
				if s, ok := al.(string); ok {
					c.AccessLevels = append(c.AccessLevels, s)
				}
			}
		}
	}

	return c
}
