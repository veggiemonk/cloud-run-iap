package iap

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// makeJWT creates a test JWT with the given header and payload maps.
func makeJWT(t *testing.T, header, payload map[string]any) string {
	t.Helper()
	h, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("failed to marshal header: %v", err)
	}
	p, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(h) + "." +
		base64.RawURLEncoding.EncodeToString(p) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
}

func TestDecode_ValidJWT(t *testing.T) {
	v := &Verifier{}
	jwt := makeJWT(t,
		map[string]any{"alg": "ES256", "typ": "JWT"},
		map[string]any{
			"iss":   "https://cloud.google.com/iap",
			"sub":   "12345",
			"email": "user@example.com",
			"aud":   "/projects/123/global/backendServices/456",
			"iat":   1700000000.0,
			"exp":   1700003600.0,
		},
	)

	result := v.Decode(jwt)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Valid {
		t.Error("Decode should not set Valid=true (no signature verification)")
	}
	if result.Claims == nil {
		t.Fatal("expected claims to be parsed")
	}
	if result.Claims.Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got: %s", result.Claims.Email)
	}
	if result.Claims.Issuer != "https://cloud.google.com/iap" {
		t.Errorf("expected issuer https://cloud.google.com/iap, got: %s", result.Claims.Issuer)
	}
	if result.Claims.Subject != "12345" {
		t.Errorf("expected subject 12345, got: %s", result.Claims.Subject)
	}
	if result.Header == nil {
		t.Fatal("expected header to be parsed")
	}
	if result.Header["alg"] != "ES256" {
		t.Errorf("expected alg ES256, got: %v", result.Header["alg"])
	}
	if result.SignatureB64 == "" {
		t.Error("expected signature to be set")
	}
}

func TestDecode_InvalidFormat(t *testing.T) {
	v := &Verifier{}

	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"no dots", "nodots", "invalid JWT format"},
		{"one dot", "one.dot", "invalid JWT format"},
		{"bad base64 header", "!!!.cGF5bG9hZA.sig", "failed to decode JWT header"},
		{"bad base64 payload", "aGVhZGVy.!!!.sig", "failed to decode JWT payload"},
		{"invalid header json", base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".cA.sig", "failed to parse JWT header JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.Decode(tt.token)
			if result.Error == "" {
				t.Fatal("expected error")
			}
			if !strings.Contains(result.Error, tt.want) {
				t.Errorf("expected error containing %q, got: %s", tt.want, result.Error)
			}
		})
	}
}

func TestVerify_NoAudience(t *testing.T) {
	v := &Verifier{expectedAudience: ""}
	jwt := makeJWT(t,
		map[string]any{"alg": "ES256"},
		map[string]any{"email": "user@example.com"},
	)

	result := v.Verify(t.Context(), jwt)

	if result.Valid {
		t.Error("expected Valid=false when no audience configured")
	}
	if !strings.Contains(result.Error, "no IAP_AUDIENCE configured") {
		t.Errorf("expected audience error, got: %s", result.Error)
	}
	// Claims should still be decoded even though verification failed.
	if result.Claims == nil {
		t.Error("expected claims to be decoded despite verification failure")
	}
}

func TestExpectedAudience(t *testing.T) {
	v := &Verifier{expectedAudience: "test-audience"}
	if v.ExpectedAudience() != "test-audience" {
		t.Errorf("expected test-audience, got: %s", v.ExpectedAudience())
	}
}

func TestParseClaims_AllFields(t *testing.T) {
	payload := map[string]any{
		"iss":   "https://cloud.google.com/iap",
		"sub":   "subject-123",
		"email": "user@example.com",
		"hd":    "example.com",
		"aud":   "/projects/123/global/backendServices/456",
		"iat":   1700000000.0,
		"exp":   1700003600.0,
		"google": map[string]any{
			"access_levels": []any{"level1", "level2"},
		},
	}

	c := parseClaims(payload)

	if c.Issuer != "https://cloud.google.com/iap" {
		t.Errorf("wrong issuer: %s", c.Issuer)
	}
	if c.Subject != "subject-123" {
		t.Errorf("wrong subject: %s", c.Subject)
	}
	if c.Email != "user@example.com" {
		t.Errorf("wrong email: %s", c.Email)
	}
	if c.HostedDomain != "example.com" {
		t.Errorf("wrong hosted domain: %s", c.HostedDomain)
	}
	if c.Audience != "/projects/123/global/backendServices/456" {
		t.Errorf("wrong audience: %s", c.Audience)
	}
	if len(c.AccessLevels) != 2 || c.AccessLevels[0] != "level1" {
		t.Errorf("wrong access levels: %v", c.AccessLevels)
	}
	if c.IssuedAt.Unix() != 1700000000 {
		t.Errorf("wrong iat: %v", c.IssuedAt)
	}
	if c.ExpiresAt.Unix() != 1700003600 {
		t.Errorf("wrong exp: %v", c.ExpiresAt)
	}
}

func TestParseClaims_EmptyPayload(t *testing.T) {
	c := parseClaims(map[string]any{})
	if c.Issuer != "" || c.Email != "" || c.Subject != "" {
		t.Error("expected empty claims from empty payload")
	}
}
