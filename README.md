# Cloud Run Auth Models Gallery

A monorepo demonstrating different authentication models for Google Cloud Run. Each app is a self-contained diagnostic tool that shows how a specific auth model works, what headers and tokens are involved, and what can go wrong.

**Module:** `github.com/veggiemonk/cloud-run-auth`

---

## Which Auth Model Should You Use?

```
Do your users need to access GCP resources on their behalf?
│
├── YES → Use OAuth (cmd/runoauth/)
│         The app manages login, obtains access tokens,
│         and calls GCP APIs on behalf of the user.
│
└── NO  → Use IAP (cmd/runiap/)
          Google manages login. IAP gates access to your app.
          Your app receives a verified identity token.
```

### Comparison

| | **IAP** | **OAuth** |
|---|---|---|
| **Auth mechanism** | Google Identity-Aware Proxy (sidecar) | OAuth 2.0 authorization code flow |
| **Token type** | Identity token (JWT signed by Google) | Access token (opaque, scoped) |
| **GCP API access** | No — the IAP JWT cannot call GCP APIs | Yes — access tokens authorize API calls |
| **Who manages login** | IAP handles the redirect and consent | Your app implements the OAuth flow |
| **Session management** | IAP manages sessions automatically | Your app manages sessions and token refresh |
| **Setup complexity** | Low — enable IAP flag, set IAM policy | Medium — configure OAuth consent screen, client ID, scopes |
| **Misconfiguration risks** | Trusting unsigned headers, wrong audience, `--allow-unauthenticated` bypass | Token leakage, scope over-granting, missing state parameter |
| **When to use** | Internal tools, admin panels, dashboards where you just need to know *who* the user is | Apps that need to read/write GCP resources (Drive, BigQuery, Cloud Storage) as the user |

---

## Apps

### RunIAP — Identity-Aware Proxy on Cloud Run

**Directory:** [`cmd/runiap/`](cmd/runiap/) | **In-depth guide:** [`docs/iap-guide.md`](docs/iap-guide.md)

Demonstrates IAP-based authentication where Google manages login and your app receives verified identity tokens. Includes diagnostic pages for inspecting IAP headers, decoding JWTs, verifying audiences, and detecting misconfigurations.

**Deploy:**

```bash
./cmd/runiap/deploy-iap.sh runiap
```

The deploy script generates templ code, builds with ko, pushes to Artifact Registry, deploys to Cloud Run with IAP enabled, restricts access to your domain, and sets the `IAP_AUDIENCE` environment variable for JWT verification.

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
./cmd/runoauth/deploy-oauth.sh runoauth --secret-name internal-tools-google-oauth-secret
```

**What it demonstrates:**
- OAuth 2.0 authorization code flow with Google
- Access token acquisition and management
- Session handling (app-managed)
- Calling GCP APIs on behalf of the user

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
│   │   ├── main.go                  # HTTP server, routing, middleware
│   │   ├── Dockerfile               # Multi-stage build (Go → distroless)
│   │   └── deploy-iap.sh            # Full deploy-with-IAP script
│   └── runoauth/                    # OAuth-based auth app
│       ├── main.go                  # HTTP server, OAuth flow, routing
│       ├── Dockerfile               # Multi-stage build (Go → distroless)
│       └── deploy-oauth.sh          # Full deploy-with-OAuth script
├── internal/
│   ├── iap/                         # IAP header detection, JWT verification
│   ├── oauth/                       # OAuth flow, session management
│   ├── shared/                      # Shared middleware
│   ├── assets/                      # Static assets (CSS)
│   ├── components/                  # Templ UI components
│   └── handler/                     # HTTP handlers
├── setup-cloud-build.sh             # One-time Cloud Build setup
├── go.mod / go.sum                  # Go dependencies (single module)
├── Makefile                         # Build and dev commands
├── .goreleaser.yaml                 # Release automation with ko
└── service.yaml                     # Knative service definition (reference)
```

**Key dependencies:**
- [`google.golang.org/api/idtoken`](https://pkg.go.dev/google.golang.org/api/idtoken) — IAP JWT verification
- [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) — OAuth 2.0 flow
- [`github.com/a-h/templ`](https://templ.guide/) — Type-safe HTML templating

---

## Future Models

- **Service-to-service auth** — authenticating Cloud Run services calling each other using service account identity tokens (no user involved). Planned as a future addition.
