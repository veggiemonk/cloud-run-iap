# Cloud Run Auth Models Gallery

A monorepo demonstrating different authentication models for Google Cloud Run. Each app is a self-contained diagnostic tool that shows how a specific auth model works, what headers and tokens are involved, and what can go wrong.

**Module:** `github.com/veggiemonk/cloud-run-auth`

---

## Which Auth Model Should You Use?

```
Do your users need to access GCP resources on their behalf?
│
├── NO  → Use IAP (cmd/runiap/)
│         Google manages login. IAP gates access to your app.
│         Your app receives a verified identity token.
│
└── YES → Is this a production deployment?
          │
          ├── YES → Use OAuth Production (cmd/runoauthprod/)
          │         Encrypted Firestore sessions, CSRF protection,
          │         rate limiting, cookie hardening, token refresh.
          │
          └── NO  → Use OAuth Demo (cmd/runoauth/)
                    Learn the OAuth 2.0 flow. Understand tokens,
                    scopes, and session management basics.
```

### Comparison

| | **IAP** | **OAuth Demo** | **OAuth Production** |
|---|---|---|---|
| **Auth mechanism** | Google Identity-Aware Proxy (sidecar) | OAuth 2.0 authorization code flow | OAuth 2.0 with production hardening |
| **Token type** | Identity token (JWT signed by Google) | Access token (opaque, scoped) | Access token + encrypted session |
| **GCP API access** | No — the IAP JWT cannot call GCP APIs | Yes — access tokens authorize API calls | Yes — with automatic token refresh |
| **Who manages login** | IAP handles the redirect and consent | Your app implements the OAuth flow | Your app, with CSRF and rate limiting |
| **Session management** | IAP manages sessions automatically | In-memory (single instance only) | Firestore (multi-instance, auto-eviction) |
| **Setup complexity** | Low — enable IAP flag, set IAM policy | Medium — OAuth consent screen, client ID | Higher — Firestore, Secret Manager, encryption keys |
| **Misconfiguration risks** | Trusting unsigned headers, wrong audience, `--allow-unauthenticated` bypass | Token leakage, scope over-granting, missing state parameter | Same as OAuth + key rotation, Firestore permissions |
| **When to use** | Internal tools, admin panels, dashboards where you just need to know *who* the user is | Learning and prototyping OAuth flows | Production apps that need GCP API access as the user |

---

## Apps

### RunIAP — Identity-Aware Proxy on Cloud Run

**Directory:** [`cmd/runiap/`](cmd/runiap/) | **In-depth guide:** [`docs/iap-guide.md`](docs/iap-guide.md)

Demonstrates IAP-based authentication where Google manages login and your app receives verified identity tokens. Includes diagnostic pages for inspecting IAP headers, decoding JWTs, verifying audiences, and detecting misconfigurations.

**Deploy:**

```bash
# One-time setup (APIs, IAM, domain restriction)
./deploy/setup-iap.sh runiap

# Every deploy (build + deploy + set IAP_AUDIENCE)
./deploy/deploy-iap.sh runiap
```

The setup script enables APIs, grants IAP service agent access, and restricts domain access. The deploy script generates templ code, builds with ko, pushes to Artifact Registry, deploys to Cloud Run with IAP enabled, and sets the `IAP_AUDIENCE` environment variable for JWT verification.

**What it demonstrates:**
- IAP header inspection (`X-Goog-IAP-JWT-Assertion`, `X-Goog-Authenticated-User-Email`)
- JWT signature verification using `google.golang.org/api/idtoken`
- Audience validation
- Header spoofing detection
- Live request logging

### RunOAuth — OAuth 2.0 on Cloud Run

**Directory:** [`cmd/runoauth/`](cmd/runoauth/) | **In-depth guide:** [`docs/oauth-guide.md`](docs/oauth-guide.md)

Demonstrates OAuth-based authentication where the app manages the full OAuth 2.0 authorization code flow. The app obtains access tokens that can call GCP APIs on behalf of the authenticated user.

**Deploy:**

```bash
# One-time setup (APIs, Secret Manager access)
./deploy/setup-oauth.sh runoauth --secret-name internal-tools-google-oauth-secret

# Every deploy (build + deploy + update redirect URL)
./deploy/deploy-oauth.sh runoauth --secret-name internal-tools-google-oauth-secret
```

**What it demonstrates:**
- OAuth 2.0 authorization code flow with Google
- Access token acquisition and management
- Session handling (app-managed)
- Calling GCP APIs on behalf of the user

### RunOAuthProd — Production OAuth on Cloud Run

**Directory:** [`cmd/runoauthprod/`](cmd/runoauthprod/) | **In-depth guide:** [`docs/runoauthprod-guide.md`](docs/runoauthprod-guide.md)

Production-hardened OAuth app built on top of `cmd/runoauth/`. Adds encrypted Firestore sessions, CSRF protection, rate limiting, cookie hardening, security headers, and automatic token refresh with singleflight deduplication. Designed for real deployments where `runoauth` is too minimal.

**Deploy:**

```bash
# One-time setup (APIs, Firestore, secrets, IAM)
./deploy/setup-prod.sh runoauthprod

# Every deploy (build + deploy with secrets)
./deploy/deploy-prod.sh runoauthprod
```

**What it adds over RunOAuth:**
- Encrypted Firestore sessions (AES-256-GCM) with TTL auto-eviction
- `__Host-` cookie prefix with secure attributes
- CSRF token validation (derived from session + HMAC key)
- Two-tier rate limiting (general + auth endpoints)
- Security headers (CSP, HSTS, X-Frame-Options, etc.)
- Automatic OAuth token refresh with singleflight deduplication
- Domain restriction for allowed email addresses
- Configuration via `ardanlabs/conf/v3` with env var and JSON blob support

---

## Common Security Pitfalls

These pitfalls are cross-referenced between models so you understand how each model handles (or fails to handle) the same class of problem.

### 1. Trusting unsigned headers (IAP)

IAP injects `X-Goog-Authenticated-User-Email` and `X-Goog-Authenticated-User-ID` headers, but these can be spoofed if a request bypasses IAP. Always verify the JWT (`X-Goog-IAP-JWT-Assertion`) — it is cryptographically signed and cannot be forged.

```go
// WRONG — headers can be spoofed
email := r.Header.Get("X-Goog-Authenticated-User-Email")

// CORRECT — verify JWT first, then read email from verified claims
result, err := verifier.Verify(ctx, r.Header.Get("X-Goog-IAP-JWT-Assertion"))
email := result.Claims.Email
```

### 2. Token storage and leakage (OAuth)

OAuth access tokens authorize API calls. If leaked, an attacker can act as the user. Never log tokens, store them in URLs, or expose them to client-side JavaScript. Use secure, HTTP-only session cookies and server-side token storage.

### 3. `--allow-unauthenticated` means different things

- **IAP:** deploying with `--allow-unauthenticated` lets anyone bypass IAP entirely by calling the Cloud Run URL directly. Always use `--no-allow-unauthenticated`.
- **OAuth:** you typically *do* need `--allow-unauthenticated` because the app itself handles the login redirect. The app is publicly reachable but enforces auth in its own middleware.

This is a common source of confusion when switching between models.

### 4. Audience validation (IAP)

If you skip audience validation, a JWT issued for *any* IAP-protected service in your project could be accepted by *your* service. Always set `IAP_AUDIENCE` and verify the `aud` claim matches.

```
WRONG:  /projects/123456/iap_web/cloud_run-us-central1/services/myapp
RIGHT:  /projects/123456/locations/us-central1/services/myapp
```

### 5. Scope over-granting (OAuth)

Request only the OAuth scopes your app actually needs. Requesting broad scopes (e.g., `https://www.googleapis.com/auth/cloud-platform`) when you only need read access to Drive gives your app — and any attacker who compromises it — far more power than necessary.

---

## Architecture

```
cloud-run-auth/
├── cmd/
│   ├── runiap/                      # IAP-based auth app
│   │   └── main.go                  # HTTP server, routing, middleware
│   ├── runoauth/                    # OAuth demo app
│   │   └── main.go                  # HTTP server, OAuth flow, routing
│   └── runoauthprod/                # Production OAuth app
│       ├── main.go                  # HTTP server, middleware chain, routing
│       ├── config.go                # Configuration (ardanlabs/conf/v3)
│       ├── auth.go                  # OAuth flow with encrypted sessions
│       └── cookies.go               # Cookie helpers (__Host- prefix, secure attrs)
├── deploy/                          # Deployment scripts (setup once, deploy many)
│   ├── _common.sh                   # Shared variables + functions (sourced)
│   ├── setup-iap.sh                 # One-time: APIs, IAM, domain restriction
│   ├── deploy-iap.sh               # Every deploy: build + deploy + IAP_AUDIENCE
│   ├── setup-oauth.sh              # One-time: APIs, Secret Manager access
│   ├── deploy-oauth.sh             # Every deploy: build + deploy + redirect URL
│   ├── setup-prod.sh               # One-time: APIs, Firestore, secrets, IAM
│   ├── deploy-prod.sh              # Every deploy: build + deploy with secrets
│   └── publish.sh                   # Tag + push → GitHub Actions release
├── internal/
│   ├── iap/                         # IAP header detection, JWT verification
│   ├── oauth/                       # OAuth flow, session management (demo)
│   ├── session/                     # Firestore session store (runoauthprod)
│   ├── middleware/                   # Production middleware
│   │   ├── csrf.go                  # CSRF token validation
│   │   ├── ratelimit.go             # Two-tier rate limiting
│   │   ├── security.go              # Security headers (CSP, HSTS, etc.)
│   │   └── bodylimit.go             # Request body size limits
│   ├── shared/                      # Shared utilities
│   │   ├── components/              # Shared templ layout and types
│   │   ├── render/                  # HTML render helpers
│   │   ├── reqlog/                  # Request log ring buffer
│   │   └── middleware.go            # Common middleware (logging, recovery)
│   ├── assets/                      # Static assets
│   │   ├── assets.go                # Embedded filesystem
│   │   └── static/style.css         # Stylesheet
│   ├── components/                  # Templ UI components
│   │   ├── iapui/                   # IAP dashboard, headers, JWT, diagnostics
│   │   └── oauthui/                 # OAuth dashboard, tokens, GCP API views
│   └── handler/                     # HTTP handlers
│       ├── iaphandler/              # IAP page handlers
│       └── oauthhandler/            # OAuth page handlers
├── docs/                            # In-depth guides
│   ├── iap-guide.md                 # IAP model deep dive
│   ├── oauth-guide.md               # OAuth model deep dive
│   └── runoauthprod-guide.md        # Production OAuth guide
├── go.mod / go.sum                  # Go dependencies (single module)
├── Makefile                         # Build and dev commands
├── .goreleaser.yaml                 # Release automation with ko
└── service.yaml                     # Knative service definition (reference)
```

**Key dependencies:**
- [`google.golang.org/api/idtoken`](https://pkg.go.dev/google.golang.org/api/idtoken) — IAP JWT verification
- [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) — OAuth 2.0 flow
- [`github.com/a-h/templ`](https://templ.guide/) — Type-safe HTML templating
- [`cloud.google.com/go/firestore`](https://pkg.go.dev/cloud.google.com/go/firestore) — Encrypted session storage (runoauthprod)
- [`github.com/ardanlabs/conf/v3`](https://pkg.go.dev/github.com/ardanlabs/conf/v3) — Configuration management (runoauthprod)
- [`golang.org/x/sync/singleflight`](https://pkg.go.dev/golang.org/x/sync/singleflight) — Token refresh deduplication (runoauthprod)
- [`golang.org/x/time/rate`](https://pkg.go.dev/golang.org/x/time/rate) — Rate limiting (runoauthprod)

---

## Future Models

- **Service-to-service auth** — authenticating Cloud Run services calling each other using service account identity tokens (no user involved). Planned as a future addition.
