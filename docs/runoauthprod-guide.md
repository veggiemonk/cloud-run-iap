# RunOAuthProd — Production OAuth on Cloud Run

> This guide covers the production-hardened OAuth app — encrypted sessions, CSRF protection, rate limiting, cookie hardening, and deployment to Cloud Run. It assumes you've read the [OAuth guide](oauth-guide.md) and understand the authorization code flow, tokens, and scopes.
>
> For the demo/learning app, see `cmd/runoauth/`. For the IAP alternative, see the [IAP guide](iap-guide.md).
>
> **App location:** [`cmd/runoauthprod/`](../cmd/runoauthprod/)

---

## Table of Contents

- [What RunOAuthProd Adds (vs RunOAuth)](#what-runoauthprod-adds-vs-runoauth)
- [Configuration](#configuration)
  - [Environment Variables](#environment-variables)
  - [OAuth Config: JSON Blob vs Individual Vars](#oauth-config-json-blob-vs-individual-vars)
  - [Generating Keys](#generating-keys)
  - [Hardcoded Security Constants](#hardcoded-security-constants)
- [Session Security — Firestore + AES-256-GCM](#session-security--firestore--aes-256-gcm)
  - [Why Encrypted Sessions Matter](#why-encrypted-sessions-matter)
  - [How Encryption Works](#how-encryption-works)
  - [Firestore Document Structure](#firestore-document-structure)
  - [TTL and Auto-Eviction](#ttl-and-auto-eviction)
  - [Multi-Instance Support](#multi-instance-support)
- [Cookie Hardening](#cookie-hardening)
  - [\_\_Host- Prefix](#__host--prefix)
  - [Cookie Attributes](#cookie-attributes)
  - [Environment-Aware Naming](#environment-aware-naming)
  - [OAuth State Cookie](#oauth-state-cookie)
- [CSRF Protection](#csrf-protection)
  - [How Tokens Are Derived](#how-tokens-are-derived)
  - [Where CSRF Is Enforced](#where-csrf-is-enforced)
  - [Multi-Instance Requirement](#multi-instance-requirement)
  - [Form and Header Support](#form-and-header-support)
- [Rate Limiting](#rate-limiting)
  - [Two Tiers](#two-tiers)
  - [IP Extraction on Cloud Run](#ip-extraction-on-cloud-run)
- [Token Lifecycle](#token-lifecycle)
  - [Automatic Refresh](#automatic-refresh)
  - [Singleflight Deduplication](#singleflight-deduplication)
- [Security Headers](#security-headers)
- [Domain Restriction](#domain-restriction)
- [Middleware Chain](#middleware-chain)
- [Deployment Checklist](#deployment-checklist)
  - [Secret Manager Integration](#secret-manager-integration)
  - [Firestore Setup](#firestore-setup)
  - [Environment Variable Checklist](#environment-variable-checklist)
- [Architecture](#architecture)
- [Troubleshooting](#troubleshooting)

---

## What RunOAuthProd Adds (vs RunOAuth)

| Concern | RunOAuth | RunOAuthProd |
|---------|----------|--------------|
| **Sessions** | In-memory `map` (lost on restart) | Firestore with AES-256-GCM encryption |
| **Cookies** | `session_id`, basic attributes | `__Host-session` prefix, full hardening |
| **CSRF** | State parameter only | HMAC-SHA256 tokens on all mutating requests |
| **Rate limiting** | None | Per-IP (auth) + per-user (protected) |
| **Token refresh** | Manual | Automatic with singleflight deduplication |
| **Security headers** | None | CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy |
| **Domain restriction** | None | Server-side Google Workspace domain check |
| **Logout** | GET request | POST-only with CSRF |
| **Multi-instance** | Breaks (in-memory sessions) | Works (Firestore-backed, no affinity needed) |
| **Body size limit** | None | 10 MiB |
| **HTTP timeouts** | Go defaults | Read: 15s, Header: 5s, Idle: 60s |
| **Config** | Env vars, no validation | `ardanlabs/conf/v3`, secrets masked in logs |

**When to use which:**

- **RunOAuth** — learning, demos, prototyping. Understand the OAuth flow first.
- **RunOAuthProd** — any deployment where real users will authenticate.

---

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PROJECT_ID` | Yes | — | GCP project ID (Firestore lives here) |
| `PORT` | No | `8080` | HTTP listen port |
| `FIRESTORE_DATABASE` | No | `(default)` | Firestore database ID |
| `SESSION_ENCRYPTION_KEY` | Yes | — | Base64-encoded 32-byte AES key |
| `CSRF_KEY` | No | Random | Base64-encoded HMAC key (≥32 bytes) |
| `K_REVISION` | Auto | — | Set by Cloud Run; enables `__Host-` cookies |
| `ALLOWED_DOMAIN` | No | `myowndomain.com` | Google Workspace domain restriction |
| `AUTH_RATE_LIMIT_BURST` | No | `20` | Requests/minute per IP on auth endpoints |
| `USER_RATE_LIMIT_BURST` | No | `60` | Requests/minute per user on protected routes |
| `GOOGLE_OAUTH_CONFIG` | See below | — | Full OAuth JSON blob from Google Console |
| `GOOGLE_CLIENT_ID` | See below | — | OAuth client ID (dev fallback) |
| `GOOGLE_CLIENT_SECRET` | See below | — | OAuth client secret (dev fallback) |
| `OAUTH_REDIRECT_URL` | No | `http://localhost:8080/auth/callback` | OAuth redirect URI |

Either `GOOGLE_OAUTH_CONFIG` **or** both `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` must be set.

### OAuth Config: JSON Blob vs Individual Vars

For production, use `GOOGLE_OAUTH_CONFIG` — the full JSON downloaded from the Google Cloud Console:

```bash
# Download from: Console → APIs & Services → Credentials → Download JSON
# Then store in Secret Manager and mount as env var
GOOGLE_OAUTH_CONFIG='{"web":{"client_id":"...","client_secret":"...","redirect_uris":["..."],...}}'
```

For local development, use individual variables:

```bash
GOOGLE_CLIENT_ID="123456789.apps.googleusercontent.com"
GOOGLE_CLIENT_SECRET="GOCSPX-..."
OAUTH_REDIRECT_URL="http://localhost:8080/auth/callback"
```

If both are set, the JSON blob takes precedence. `OAUTH_REDIRECT_URL` overrides the redirect URI in the JSON blob when set.

### Generating Keys

```bash
# SESSION_ENCRYPTION_KEY — exactly 32 bytes, base64-encoded
openssl rand -base64 32

# CSRF_KEY — at least 32 bytes, base64-encoded
openssl rand -base64 32
```

```go
// WRONG — using a human-readable passphrase
SESSION_ENCRYPTION_KEY="my-super-secret-key"

// CORRECT — cryptographically random, base64-encoded
SESSION_ENCRYPTION_KEY="K7gNU3sdo+OL0wNhqoVWhr3g6s1xYv72ol/pe/Unols="
```

### Hardcoded Security Constants

These values are compiled into the binary and are **not** configurable. This is intentional — misconfiguration is a security risk:

| Constant | Value | Rationale |
|----------|-------|-----------|
| `OAuthStateCookieMaxAge` | 300 (5 min) | State should be short-lived; prevents replay |
| `SessionCookieMaxAge` | 86400 (24 h) | Limits session exposure window |
| `SessionTTL` | 24 hours | Firestore document auto-eviction |
| `TokenRefreshThreshold` | 5 minutes | Refresh before expiry, not after |
| `MaxBodyBytes` | 10 MiB | Prevents request body abuse |
| `ReadTimeout` | 15 seconds | Prevents slow-read attacks |
| `ReadHeaderTimeout` | 5 seconds | Prevents slowloris-style attacks |
| `IdleTimeout` | 60 seconds | Reclaims idle connections |
| `csrfKeyMinBytes` | 32 | Minimum HMAC key length |
| `sessionIDBytes` | 32 | 256-bit session IDs |

---

## Session Security — Firestore + AES-256-GCM

### Why Encrypted Sessions Matter

OAuth tokens are credentials — a leaked refresh token gives persistent access to the user's Google account (within consented scopes). Storing tokens in plaintext means a Firestore data breach directly exposes every user's tokens.

RunOAuthProd encrypts tokens at rest so a database breach yields only ciphertext.

### How Encryption Works

```
Encrypt:
  plaintext  ──▶  random nonce (12 bytes)  ──▶  AES-256-GCM Seal
                                                      │
                                                      ▼
                                            nonce || ciphertext
                                            (stored in Firestore)

Decrypt:
  nonce || ciphertext  ──▶  split at 12 bytes  ──▶  AES-256-GCM Open
                                                          │
                                                          ▼
                                                      plaintext
```

- **Algorithm:** AES-256 in Galois/Counter Mode (GCM) — authenticated encryption
- **Nonce:** 12 bytes from `crypto/rand`, unique per encryption operation
- **Storage format:** `nonce (12 bytes) || ciphertext`
- **Tamper detection:** GCM authenticates the ciphertext — any modification causes decryption failure

Each encryption produces different ciphertext even for the same plaintext (random nonce), which prevents pattern analysis.

### Firestore Document Structure

Collection: `sessions`

| Field | Type | Encrypted | Description |
|-------|------|-----------|-------------|
| `Email` | string | No | User's email address |
| `Name` | string | No | User's display name |
| `Picture` | string | No | Profile picture URL (HTTPS only) |
| `EncryptedAccessToken` | bytes | Yes | AES-256-GCM ciphertext |
| `EncryptedRefreshToken` | bytes | Yes | AES-256-GCM ciphertext |
| `TokenExpiry` | timestamp | No | When the access token expires |
| `CreatedAt` | timestamp | No | Session creation time |
| `ExpiresAt` | timestamp | No | Session expiry (CreatedAt + 24h) |

Only the OAuth tokens are encrypted — email/name are stored in plaintext for potential admin queries. The session ID (document key) is a 32-byte random value, base64 URL-encoded.

### TTL and Auto-Eviction

Sessions expire 24 hours after creation. The app enforces this in two ways:

1. **Application-level:** `Store.Get()` checks `ExpiresAt` — if expired, it deletes the document and returns nil
2. **Firestore TTL policy:** Configure a [Firestore TTL policy](https://cloud.google.com/firestore/docs/ttl) on the `ExpiresAt` field for automatic background cleanup of abandoned sessions

### Multi-Instance Support

Because sessions live in Firestore (not in memory), multiple Cloud Run instances share the same session state. No session affinity is needed — any instance can serve any request.

> **Contrast with RunOAuth:** RunOAuth's in-memory sessions break with `--max-instances > 1` unless you enable session affinity.

---

## Cookie Hardening

### \_\_Host- Prefix

On Cloud Run (detected via `K_REVISION` env var), cookies use the `__Host-` prefix:

```
__Host-session
__Host-oauth-state
```

The `__Host-` prefix tells the browser to enforce:
- `Secure` flag (HTTPS only)
- `Path=/` (sent on all paths)
- No `Domain` attribute (exact host match only — no subdomain leakage)

These constraints are browser-enforced and cannot be overridden by the server.

### Cookie Attributes

| Attribute | Value | Purpose |
|-----------|-------|---------|
| `HttpOnly` | `true` | Prevents JavaScript access (XSS mitigation) |
| `Secure` | `true` (Cloud Run) | HTTPS-only transmission |
| `SameSite` | `Lax` | Sent on top-level navigation, blocked on cross-site POSTs |
| `Path` | `/` | Available to all routes |

### Environment-Aware Naming

| Environment | Session Cookie | State Cookie |
|-------------|----------------|--------------|
| Cloud Run | `__Host-session` | `__Host-oauth-state` |
| Local dev | `session` | `oauth-state` |

Detection is automatic — Cloud Run sets `K_REVISION`, local dev doesn't.

### OAuth State Cookie

The state cookie has a 5-minute TTL and is cleared immediately after use in the callback handler:

```
Login:     Set __Host-oauth-state (MaxAge=300)
Callback:  Read state, verify, delete cookie (MaxAge=-1)
```

This prevents state reuse and limits the replay window.

---

## CSRF Protection

### How Tokens Are Derived

CSRF tokens are derived from the session ID using HMAC-SHA256:

```
CSRF token = base64url( HMAC-SHA256(csrf_key, session_id) )
```

This is deterministic — the same session always produces the same CSRF token. No server-side token storage is needed.

Validation uses `hmac.Equal` (constant-time comparison) to prevent timing attacks.

### Where CSRF Is Enforced

CSRF validation applies to all **mutating** HTTP methods:

| Method | CSRF Required |
|--------|---------------|
| GET, HEAD, OPTIONS | No |
| POST, PUT, DELETE, PATCH | Yes |

Currently, the only mutating endpoint is `POST /auth/logout`. Protected GET routes also pass through the CSRF middleware (to inject the token), but aren't validated.

### Multi-Instance Requirement

> **If you run multiple Cloud Run instances, you must set `CSRF_KEY` explicitly.**

When `CSRF_KEY` is empty, the app generates a random key at startup. Each instance generates a different key, which means a CSRF token generated by instance A will fail validation on instance B.

```bash
# WRONG for multi-instance — each instance gets a different key
CSRF_KEY=""

# CORRECT — all instances share the same key
CSRF_KEY="$(openssl rand -base64 32)"
```

### Form and Header Support

CSRF tokens can be submitted via:

| Method | Name | Usage |
|--------|------|-------|
| Form field | `csrf_token` | `<input type="hidden" name="csrf_token" value="...">` |
| HTTP header | `X-CSRF-Token` | For JavaScript/fetch requests |

The middleware checks the form field first, then falls back to the header.

---

## Rate Limiting

### Two Tiers

| Tier | Scope | Default | Applies To |
|------|-------|---------|------------|
| **IP-based** | Per IP address | 20 req/min | `/auth/login`, `/auth/callback` |
| **User-based** | Per authenticated user | 60 req/min | All protected routes (`/`, `/token`, `/gcp`, `/diagnostic`) |

Both use token-bucket rate limiting. When the limit is exceeded, the server returns **HTTP 429 Too Many Requests**.

IP-based limiting protects against brute-force login attempts. User-based limiting prevents a single authenticated user from overwhelming the backend.

### IP Extraction on Cloud Run

Cloud Run sits behind a load balancer that sets `X-Forwarded-For`. The rate limiter extracts the client IP from the first entry in this header, falling back to `RemoteAddr` if absent.

```
X-Forwarded-For: <client-ip>, <load-balancer-ip>
                  ^^^^^^^^^^^
                  used for rate limiting
```

---

## Token Lifecycle

### Automatic Refresh

The `requireAuthWithRefresh` middleware checks token expiry on every request to protected routes:

```
Request arrives
    │
    ▼
Token expires in > 5 minutes?  ──yes──▶  Proceed normally
    │
    no
    │
    ▼
Refresh token exists?  ──no──▶  Proceed with current token
    │
    yes
    │
    ▼
Refresh via singleflight  ──▶  Update encrypted token in Firestore
    │
    ▼
Proceed with new token
```

If the refresh fails, the request continues with the old token and a warning is logged. This prevents a transient Google outage from breaking all authenticated requests.

### Singleflight Deduplication

When multiple requests for the same session arrive simultaneously and the token needs refreshing, `singleflight.Group` ensures only **one** refresh call is made to Google:

```
Request A ─┐
Request B ─┤── singleflight.Do(sessionID) ──▶  Single refresh call to Google
Request C ─┘                                        │
                                                    ▼
            All three get the same new token
```

This prevents the "thundering herd" problem where N concurrent requests trigger N redundant token refreshes.

---

## Security Headers

Every response includes these headers (via the `SecurityHeaders` middleware):

| Header | Value | Purpose |
|--------|-------|---------|
| `Content-Security-Policy` | `default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://*.googleusercontent.com; script-src 'self'; frame-ancestors 'none'` | Restricts resource loading; `frame-ancestors 'none'` replaces X-Frame-Options in modern browsers |
| `X-Content-Type-Options` | `nosniff` | Prevents MIME-type sniffing |
| `X-Frame-Options` | `DENY` | Prevents clickjacking (legacy browsers) |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | Limits referrer leakage to other origins |

The CSP allows Google profile pictures (`https://*.googleusercontent.com`) and inline styles (required for the template engine). Scripts are restricted to same-origin.

---

## Domain Restriction

RunOAuthProd restricts login to a single Google Workspace domain using **two** mechanisms:

```
1. OAuth hint (client-side):
   oauth2.SetAuthURLParam("hd", allowedDomain)
   ↳ Google pre-filters the account picker to the specified domain

2. Server-side verification (callback):
   if userInfo.HD != cfg.AllowedDomain → 403 Forbidden
   ↳ Checks the hd claim in the userinfo response
```

**Why both are needed:**

The `hd` parameter is a hint — it tells Google to show the right domain's login screen, but **does not prevent** a user from authenticating with a different account. An attacker could craft an OAuth URL without the `hd` hint, or use a personal Google account. Only the server-side check in the callback handler is authoritative.

```go
// WRONG — trusting the hd hint alone
url := cfg.AuthCodeURL(state, oauth2.SetAuthURLParam("hd", "example.com"))
// (no server-side check)

// CORRECT — hint for UX, server-side check for security
url := cfg.AuthCodeURL(state, oauth2.SetAuthURLParam("hd", "example.com"))
// In callback:
if userInfo.HD != "example.com" {
    http.Error(w, "Forbidden: domain not allowed", http.StatusForbidden)
    return
}
```

---

## Middleware Chain

Requests pass through middleware from outermost to innermost:

```
Request
  │
  ▼
┌─────────────────────────────────────────────┐
│ 1. LoggingMiddleware                        │  JSON structured logging
├─────────────────────────────────────────────┤
│ 2. SecurityHeaders                          │  CSP, X-Frame-Options, etc.
├─────────────────────────────────────────────┤
│ 3. MaxBodySize (10 MiB)                     │  Reject oversized bodies
├─────────────────────────────────────────────┤
│ 4. RequestLogMiddleware                     │  Ring buffer for diagnostics
├─────────────────────────────────────────────┤
│ Router: /auth/* vs protected vs /healthz    │
├──────────────┬──────────────────────────────┤
│ Auth routes: │ Protected routes:            │
│              │                              │
│ 5. IP rate   │ 5. requireAuthWithRefresh    │  Session lookup + token refresh
│    limiter   │ 6. User rate limiter         │  Per-user throttle
│ 6. Handler   │ 7. CSRF RequireCSRF          │  Validate mutating requests
│              │ 8. Handler                   │
└──────────────┴──────────────────────────────┘
```

**Why ordering matters:**

- Security headers apply to **all** responses, even errors — they must be outermost
- Body size is checked before any parsing — prevents memory exhaustion
- Auth middleware runs before rate limiting (user limiter needs the user identity)
- CSRF is innermost on protected routes — it needs the session ID from auth middleware

---

## Deployment Checklist

### Secret Manager Integration

Store OAuth credentials in Secret Manager and mount as an environment variable:

```bash
# 1. Create the secret from the downloaded OAuth JSON
gcloud secrets create oauth-config \
  --data-file=client_secret_*.json

# 2. Deploy with the secret mounted as an env var
gcloud run deploy runoauthprod \
  --set-secrets="GOOGLE_OAUTH_CONFIG=oauth-config:latest"
```

For `SESSION_ENCRYPTION_KEY` and `CSRF_KEY`:

```bash
# Generate and store encryption key
openssl rand -base64 32 | gcloud secrets create session-key --data-file=-

# Generate and store CSRF key
openssl rand -base64 32 | gcloud secrets create csrf-key --data-file=-

# Mount both in deploy
gcloud run deploy runoauthprod \
  --set-secrets="SESSION_ENCRYPTION_KEY=session-key:latest,CSRF_KEY=csrf-key:latest,GOOGLE_OAUTH_CONFIG=oauth-config:latest"
```

### Firestore Setup

```bash
# 1. Enable the Firestore API
gcloud services enable firestore.googleapis.com

# 2. Create the database (if not using default)
gcloud firestore databases create --database="(default)" --location=us-central1

# 3. (Recommended) Set up TTL policy for automatic session cleanup
gcloud firestore fields ttls update ExpiresAt \
  --collection-group=sessions \
  --enable-ttl
```

The app will create the `sessions` collection automatically on first use.

### Environment Variable Checklist

Before deploying, verify all required configuration:

1. `PROJECT_ID` — GCP project ID
2. `SESSION_ENCRYPTION_KEY` — base64-encoded 32-byte key (via Secret Manager)
3. `GOOGLE_OAUTH_CONFIG` — OAuth JSON blob (via Secret Manager)
4. `OAUTH_REDIRECT_URL` — `https://<service-url>/auth/callback`
5. `ALLOWED_DOMAIN` — your Google Workspace domain
6. `CSRF_KEY` — base64-encoded key (via Secret Manager, **required for multi-instance**)
7. OAuth redirect URI added to [Google Cloud Console credentials](https://console.cloud.google.com/apis/credentials)
8. Firestore database exists in the same project
9. Cloud Run service account has Firestore read/write access

---

## Architecture

```
cmd/runoauthprod/
├── main.go          # Server setup, routing, middleware chain, OAuth config
├── config.go        # Environment config (ardanlabs/conf), hardcoded constants
├── auth.go          # Login, callback, logout handlers; token refresh; domain check
└── cookies.go       # Cookie config (__Host- prefix, environment detection)

internal/
├── middleware/
│   ├── csrf.go      # HMAC-SHA256 CSRF tokens, RequireCSRF middleware
│   ├── ratelimit.go # IP and user-based token bucket rate limiters
│   ├── security.go  # Security headers (CSP, X-Frame-Options, etc.)
│   └── bodylimit.go # MaxBodySize middleware
├── session/
│   └── firestore.go # Firestore session store with AES-256-GCM encryption
├── handler/oauthhandler/
│   ├── dashboard.go # User dashboard
│   ├── token.go     # Token inspector
│   ├── gcp.go       # GCP project explorer
│   ├── diagnostic.go# Health checks
│   └── healthz.go   # Health check endpoint
└── shared/          # Logging, request log, templating (shared with runoauth)
```

**Key dependencies:**

- [`ardanlabs/conf/v3`](https://pkg.go.dev/github.com/ardanlabs/conf/v3) — Environment-based configuration
- [`cloud.google.com/go/firestore`](https://pkg.go.dev/cloud.google.com/go/firestore) — Session storage
- [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) — OAuth 2.0 flow
- [`golang.org/x/sync/singleflight`](https://pkg.go.dev/golang.org/x/sync/singleflight) — Token refresh deduplication
- [`golang.org/x/time/rate`](https://pkg.go.dev/golang.org/x/time/rate) — Token bucket rate limiting
- [`crypto/aes`](https://pkg.go.dev/crypto/aes) + [`crypto/cipher`](https://pkg.go.dev/crypto/cipher) — AES-256-GCM encryption

---

## Troubleshooting

### Session lost after deploying a new revision

**Cause:** You rotated `SESSION_ENCRYPTION_KEY`. All existing sessions were encrypted with the old key and can no longer be decrypted.

**Fix:** Users will need to log in again. To avoid this during key rotation, implement a two-key strategy (try new key, fall back to old key) or drain sessions before rotation.

### CSRF validation failures across instances

**Cause:** `CSRF_KEY` is empty, so each Cloud Run instance generates its own random key.

**Fix:** Set `CSRF_KEY` explicitly via Secret Manager. All instances must share the same key.

```bash
openssl rand -base64 32 | gcloud secrets create csrf-key --data-file=-
# Then mount as CSRF_KEY in your Cloud Run deployment
```

### 403 Forbidden on callback — "domain not allowed"

**Cause:** The user authenticated with a Google account outside your `ALLOWED_DOMAIN`.

**Fix:** Verify the user's Google Workspace domain matches `ALLOWED_DOMAIN`. If you need to allow multiple domains, the current implementation supports only one — you'd need to modify the domain check in `auth.go`.

### 429 Too Many Requests

**Cause:** Rate limiter triggered.

**Fix:** Check which tier is triggering:
- Auth endpoints (IP-based): Default 20 req/min. Increase `AUTH_RATE_LIMIT_BURST` if legitimate.
- Protected routes (user-based): Default 60 req/min. Increase `USER_RATE_LIMIT_BURST` if legitimate.

### "SESSION_ENCRYPTION_KEY must be a base64-encoded 32-byte key"

**Cause:** The key is not valid base64 or doesn't decode to exactly 32 bytes.

**Fix:**

```bash
# Generate a correct key
openssl rand -base64 32
# Output example: K7gNU3sdo+OL0wNhqoVWhr3g6s1xYv72ol/pe/Unols=

# Verify it decodes to 32 bytes
echo "K7gNU3sdo+OL0wNhqoVWhr3g6s1xYv72ol/pe/Unols=" | base64 -d | wc -c
# Should output: 32
```

### Cookies not being set in production

**Cause:** The `__Host-` prefix requires HTTPS. If your service isn't behind TLS, the browser will reject the cookies silently.

**Fix:** Cloud Run provides TLS by default. If you're running behind a custom proxy, ensure it terminates TLS before the request reaches the app. For local development, cookies automatically use the non-prefixed names (no HTTPS required).

### Token refresh fails silently

**Cause:** The refresh token may have been revoked, or the OAuth credentials changed.

**Fix:** Check the application logs for `"failed to refresh token"` warnings. The app continues with the old token — the user will eventually get an API error and need to re-authenticate. If this happens consistently, verify your OAuth credentials are still valid.
