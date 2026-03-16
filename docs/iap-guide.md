# IAP on Cloud Run — In-Depth Guide

> This guide was the original README for the RunIAP app. It covers everything about deploying Cloud Run services behind Google Cloud's [Identity-Aware Proxy (IAP)](https://cloud.google.com/iap/docs/concepts-overview) — how IAP works, deployment, JWT verification, security pitfalls, troubleshooting, and integration examples in Go, Python, and Node.js.
>
> For the project overview and auth model comparison, see the [top-level README](../README.md).
>
> **App location:** [`cmd/runiap/`](../cmd/runiap/)

**Official docs:**
- [IAP on Cloud Run](https://cloud.google.com/run/docs/securing/identity-aware-proxy-cloud-run)
- [IAP blog post](https://cloud.google.com/blog/products/serverless/iap-integration-with-cloud-run)

---

## Table of Contents

- [Quick Start](#quick-start)
- [How IAP Works on Cloud Run](#how-iap-works-on-cloud-run)
- [Deployment Guide](#deployment-guide)
  - [Prerequisites](#prerequisites)
  - [One-Time Project Setup](#one-time-project-setup)
  - [Deploy with IAP](#deploy-with-iap)
  - [Step-by-Step Manual Deployment](#step-by-step-manual-deployment)
- [Understanding IAP Headers](#understanding-iap-headers)
  - [The Three Headers](#the-three-headers)
  - [Why Headers Alone Are Not Enough](#why-headers-alone-are-not-enough)
- [JWT Verification](#jwt-verification)
  - [What the JWT Contains](#what-the-jwt-contains)
  - [Verifying the JWT in Your App](#verifying-the-jwt-in-your-app)
  - [The Audience Value](#the-audience-value)
  - [Go Implementation](#go-implementation)
- [Common Security Mistakes](#common-security-mistakes)
- [RunIAP Diagnostic Pages](#runiap-diagnostic-pages)
- [Integrating IAP in Your Own App](#integrating-iap-in-your-own-app)
  - [Minimal Go Example](#minimal-go-example)
  - [Python / Flask Example](#python--flask-example)
  - [Node.js / Express Example](#nodejs--express-example)
- [Troubleshooting](#troubleshooting)
- [Architecture](#architecture)

---

## Quick Start

```bash
# One-time setup (APIs, IAM, domain restriction)
./deploy/setup-iap.sh runiap

# Every deploy (build + deploy + set IAP_AUDIENCE)
./deploy/deploy-iap.sh runiap
```

The setup script enables APIs, grants IAP service agent access, and restricts domain access. The deploy script generates templ code, builds with [ko](https://ko.build/), pushes to Artifact Registry, deploys to Cloud Run with IAP enabled, and sets the correct `IAP_AUDIENCE` environment variable.

---

## How IAP Works on Cloud Run

```
┌──────────┐     ┌─────────────────────┐     ┌──────────────────┐
│  Browser  │────▶│  Identity-Aware     │────▶│  Cloud Run       │
│           │◀────│  Proxy (IAP)        │◀────│  Service         │
└──────────┘     └─────────────────────┘     └──────────────────┘
                   │                           │
                   │ 1. Intercepts request     │ 4. Receives request
                   │ 2. Redirects to Google    │    with IAP headers
                   │    sign-in if needed      │ 5. Verifies JWT
                   │ 3. Validates identity     │ 6. Extracts user
                   │    and authorization      │    identity
                   │                           │
                   ▼                           ▼
              Only authorized             App can trust the
              users get through           identity in the JWT
```

When you enable IAP on a Cloud Run service:

1. **Every request** is intercepted by IAP before it reaches your container.
2. **Unauthenticated users** are redirected to Google's OAuth consent screen.
3. **Unauthorized users** (not matching your IAM policy) get a 403 error from IAP — your app never sees the request.
4. **Authorized users** have their request forwarded to your app with three additional headers injected by IAP.

Cloud Run's native IAP integration (the `--iap` flag) does this without needing a load balancer. IAP runs as a sidecar alongside your container.

---

## Deployment Guide

### Prerequisites

- [Google Cloud SDK (`gcloud`)](https://cloud.google.com/sdk/docs/install) installed and authenticated
- A GCP project with billing enabled
- [ko](https://ko.build/) installed (`go install github.com/google/ko@latest`)
- Docker is **not** required locally — ko builds Go images directly

### Deploy with IAP

```bash
# One-time setup
./deploy/setup-iap.sh <service-name> [--region <region>] [--domain <domain>]

# Every deploy
./deploy/deploy-iap.sh <service-name> [--region <region>] [--port <port>]
```

**Examples:**

```bash
# One-time setup with defaults (us-central1, myowndomain.com)
./deploy/setup-iap.sh runiap

# Setup with custom domain
./deploy/setup-iap.sh runiap --domain example.com

# Deploy with defaults (us-central1, port 8080)
./deploy/deploy-iap.sh runiap

# Deploy with custom region and port
./deploy/deploy-iap.sh runiap --region europe-west1 --port 3000
```

**What the setup script does:**

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `gcloud services enable ...` | Enable IAP, Cloud Run, Artifact Registry APIs |
| 2 | `gcloud run services add-iam-policy-binding ...` | Let the IAP service agent invoke your Cloud Run service |
| 3 | `gcloud iap web add-iam-policy-binding ...` | Restrict IAP access to your domain |

**What the deploy script does:**

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `templ generate` + `ko build` | Generate templ code, build Go image with ko, push to Artifact Registry |
| 2 | `gcloud run deploy --image ... --iap` | Deploy the ko-built image with IAP enabled |
| 3 | `gcloud run services update --update-env-vars ...` | Set `IAP_AUDIENCE` for JWT verification |

### Step-by-Step Manual Deployment

If you prefer to run the steps manually or need to customize them:

#### 1. Enable APIs

```bash
gcloud services enable \
  iap.googleapis.com \
  run.googleapis.com \
  artifactregistry.googleapis.com
```

#### 2. Build and push the image with ko

```bash
# Generate templ code first
go tool templ generate

# Build and push (set KO_DOCKER_REPO to your Artifact Registry including image name)
PROJECT_ID=$(gcloud config get-value project)
export KO_DOCKER_REPO="us-central1-docker.pkg.dev/${PROJECT_ID}/cloud-run-source-deploy/runiap"
ko build ./cmd/runiap --bare --platform=linux/amd64
```

#### 3. Deploy the image with IAP

```bash
IMAGE="${KO_DOCKER_REPO}"
gcloud run deploy myapp \
  --image="$IMAGE" \
  --region=us-central1 \
  --no-allow-unauthenticated \
  --iap \
  --port=8080
```

Key flags:
- `--image` — deploy the ko-built image from Artifact Registry
- `--no-allow-unauthenticated` — blocks direct access without authentication
- `--iap` — enables IAP as a sidecar on the Cloud Run service

#### 4. Grant IAP service agent the invoker role

IAP needs permission to forward requests to your Cloud Run service:

```bash
PROJECT_NUMBER=$(gcloud projects describe $(gcloud config get-value project) --format='value(projectNumber)')

gcloud run services add-iam-policy-binding myapp \
  --region=us-central1 \
  --member=serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com \
  --role=roles/run.invoker
```

Without this, IAP can authenticate users but can't forward their requests — you'll see 403 errors even for authorized users.

#### 5. Set the IAM policy for who can access

Grant access to a domain, group, or individual:

```bash
# Entire domain
gcloud iap web add-iam-policy-binding \
  --member=domain:example.com \
  --role=roles/iap.httpsResourceAccessor \
  --region=us-central1 \
  --resource-type=cloud-run \
  --service=myapp

# Specific group
gcloud iap web add-iam-policy-binding \
  --member=group:engineering@example.com \
  --role=roles/iap.httpsResourceAccessor \
  --region=us-central1 \
  --resource-type=cloud-run \
  --service=myapp

# Individual user
gcloud iap web add-iam-policy-binding \
  --member=user:alice@example.com \
  --role=roles/iap.httpsResourceAccessor \
  --region=us-central1 \
  --resource-type=cloud-run \
  --service=myapp
```

#### 6. Set the IAP_AUDIENCE environment variable

Your app needs this to verify JWT signatures:

```bash
PROJECT_NUMBER=$(gcloud projects describe $(gcloud config get-value project) --format='value(projectNumber)')

gcloud run services update myapp \
  --region=us-central1 \
  --update-env-vars=IAP_AUDIENCE=/projects/${PROJECT_NUMBER}/locations/us-central1/services/myapp
```

The audience format for Cloud Run native IAP is:

```
/projects/{PROJECT_NUMBER}/locations/{REGION}/services/{SERVICE_NAME}
```

> **Warning:** The audience is NOT the IAP resource path (`/projects/.../iap_web/...`).
> It's the Cloud Run service path (`/projects/.../locations/.../services/...`).
> Using the wrong format will cause "audience mismatch" errors during JWT verification.

---

## Understanding IAP Headers

### The Three Headers

When a request passes through IAP, three headers are added:

| Header | Content | Example |
|--------|---------|---------|
| `X-Goog-IAP-JWT-Assertion` | Signed JWT token | `eyJhbGciOiJFUzI1NiI...` |
| `X-Goog-Authenticated-User-Email` | User's email (prefixed) | `accounts.google.com:alice@example.com` |
| `X-Goog-Authenticated-User-ID` | User's unique ID (prefixed) | `accounts.google.com:118249055953592475869` |

The email and ID headers have a `accounts.google.com:` prefix that you need to strip.

### Why Headers Alone Are Not Enough

**The email and ID headers can be spoofed.** Any HTTP client can set arbitrary request headers. If your app only checks `X-Goog-Authenticated-User-Email` without verifying the JWT, an attacker who can reach your service directly could impersonate any user by setting that header.

```
INSECURE — DO NOT DO THIS:

  email := r.Header.Get("X-Goog-Authenticated-User-Email")
  // Trusting this header without JWT verification is dangerous!
```

IAP removes these headers from incoming requests and re-adds them, but only if the request actually goes through IAP. If there's ever a misconfiguration that allows direct access to the service, those headers won't be stripped.

**Always verify the JWT.** The JWT is cryptographically signed by Google and cannot be forged. It is the only trustworthy source of user identity.

RunIAP detects this dangerous state and displays a warning: *"IAP email/ID headers present without JWT assertion. These headers can be spoofed if IAP is not properly configured."*

---

## JWT Verification

### What the JWT Contains

The IAP JWT is a standard [JSON Web Token](https://jwt.io/) signed with ES256 (ECDSA P-256). Here's an example payload:

```json
{
  "aud": "/projects/628205712753/locations/us-central1/services/runiap",
  "azp": "/projects/628205712753/locations/us-central1/services/runiap",
  "email": "alice@example.com",
  "exp": 1773526816,
  "google": {
    "access_levels": [
      "accessPolicies/123456/accessLevels/my_level"
    ],
    "device_id": "gnVNhYoxkOfuqnDEj1jxn2iHm4aWRzzsI20es1yPw-U"
  },
  "hd": "example.com",
  "iat": 1773526216,
  "identity_source": "GOOGLE",
  "iss": "https://cloud.google.com/iap",
  "sub": "accounts.google.com:118249055953592475869"
}
```

**Key claims:**

| Claim | Meaning |
|-------|---------|
| `iss` | Issuer — always `https://cloud.google.com/iap` |
| `aud` | Audience — must match your `IAP_AUDIENCE` |
| `email` | Authenticated user's email |
| `hd` | Hosted domain (Google Workspace domain) |
| `sub` | Unique, stable user identifier |
| `iat` | Issued-at timestamp (Unix epoch) |
| `exp` | Expiration timestamp (Unix epoch) |
| `google.access_levels` | [Context-Aware Access](https://cloud.google.com/access-context-manager/docs) levels (if configured) |
| `google.device_id` | Device identifier (if using BeyondCorp) |

### Verifying the JWT in Your App

Verification consists of three checks:

1. **Signature** — The JWT is signed by Google using ES256. Verify the signature against [Google's public keys](https://www.gstatic.com/iap/verify/public_key-jwk).
2. **Audience** — The `aud` claim must match your expected value. This prevents a JWT issued for a different service from being accepted by yours.
3. **Expiration** — The `exp` claim must be in the future.

Most Google auth libraries handle all three. You provide the expected audience, and the library does the rest.

### The Audience Value

For Cloud Run services with native IAP (the `--iap` flag), the audience follows this format:

```
/projects/{PROJECT_NUMBER}/locations/{REGION}/services/{SERVICE_NAME}
```

For example:
```
/projects/628205712753/locations/us-central1/services/runiap
```

> **Common mistake:** Using the IAP resource path (`/projects/.../iap_web/cloud_run-REGION/services/...`) instead of the Cloud Run location path. These look similar but are different. The JWT's `aud` claim uses the location format.

You can always find the correct value by decoding an actual JWT from your running service (the RunIAP JWT page does this automatically).

### Go Implementation

RunIAP uses Google's `google.golang.org/api/idtoken` package for verification:

```go
package iap

import (
    "context"
    "os"

    "google.golang.org/api/idtoken"
)

type Verifier struct {
    expectedAudience string
}

func NewVerifier() *Verifier {
    return &Verifier{
        expectedAudience: os.Getenv("IAP_AUDIENCE"),
    }
}

func (v *Verifier) Verify(ctx context.Context, rawJWT string) (*VerificationResult, error) {
    if v.expectedAudience == "" {
        return nil, fmt.Errorf("no IAP_AUDIENCE configured")
    }

    payload, err := idtoken.Validate(ctx, rawJWT, v.expectedAudience)
    if err != nil {
        return &VerificationResult{Valid: false, Error: err.Error()}, nil
    }

    // payload.Claims contains the verified JWT claims
    return &VerificationResult{
        Valid:   true,
        Payload: payload.Claims,
        Claims:  parseClaims(payload.Claims),
    }, nil
}
```

`idtoken.Validate` handles:
- Fetching and caching Google's public keys
- Verifying the ES256 signature
- Checking the audience matches
- Checking the token hasn't expired

---

## Common Security Mistakes

### 1. Trusting headers without verifying the JWT

```go
// WRONG — headers can be spoofed
email := r.Header.Get("X-Goog-Authenticated-User-Email")
```

```go
// CORRECT — verify JWT first, then read email from claims
result, err := verifier.Verify(ctx, r.Header.Get("X-Goog-IAP-JWT-Assertion"))
if err != nil || !result.Valid {
    http.Error(w, "Unauthorized", 401)
    return
}
email := result.Claims.Email
```

### 2. Not verifying the audience

If you skip audience validation, a JWT from *any* IAP-protected service in your project could be used to access *your* service. Always configure `IAP_AUDIENCE`.

### 3. Using `--allow-unauthenticated` with IAP

If you deploy with `--allow-unauthenticated`, anyone can bypass IAP by calling the Cloud Run URL directly. Always use `--no-allow-unauthenticated`.

### 4. Wrong audience format

```
WRONG:  /projects/628205712753/iap_web/cloud_run-us-central1/services/runiap
RIGHT:  /projects/628205712753/locations/us-central1/services/runiap
```

### 5. Forgetting the IAP service agent binding

Without granting `roles/run.invoker` to `service-{PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com`, IAP can't forward requests to your service. Users will be authenticated but get 403 errors.

### 6. Assuming the IAP JWT is a GCP access token

The IAP JWT is an **identity token**, not an access token. It proves who the user is to your backend, but it **cannot** be used to call GCP APIs (Cloud Storage, BigQuery, Pub/Sub, etc.) on the user's behalf.

| Token type | Purpose | Can call GCP APIs? |
|---|---|---|
| IAP JWT (`X-Goog-IAP-JWT-Assertion`) | Proves user identity to your app | No |
| OAuth2 access token | Authorizes API calls on behalf of a user or service account | Yes |

If your app needs to access GCP resources:
- **As the app itself** — use the Cloud Run service's [service account identity](https://cloud.google.com/run/docs/securing/service-identity). No extra setup needed; the default service account credentials are available automatically.
- **As the authenticated user** — you need a separate [OAuth2 consent flow](https://cloud.google.com/iap/docs/authentication-howto#authenticating_from_a_service_account) to obtain an access token. IAP does not forward the user's OAuth2 token.

### 7. Hardcoding the audience

Don't hardcode the audience string in your code. Use an environment variable (`IAP_AUDIENCE`) so the same image works across environments.

---

## RunIAP Diagnostic Pages

RunIAP provides six pages for understanding and debugging IAP:

| Page | Path | Purpose |
|------|------|---------|
| **Dashboard** | `/` | At-a-glance status: user identity, IAP presence, JWT validity |
| **Headers** | `/headers` | All request headers with IAP headers (`x-goog-*`) highlighted |
| **JWT** | `/jwt` | Decoded JWT: header, payload, signature, parsed claims |
| **Audience** | `/audience` | Compare JWT audience against expected value |
| **Log** | `/log` | Live timeline of the last 200 requests with identity info |
| **Diagnostic** | `/diagnostic` | Automated health checks: JWT presence, signature, issuer, expiry, email consistency, bypass risk |

Every page supports JSON output by appending `?format=json` or setting `Accept: application/json` — useful for scripted testing.

The **Diagnostic** page runs these checks:

| Check | Pass condition |
|-------|----------------|
| JWT Present | `X-Goog-IAP-JWT-Assertion` header exists |
| Signature Valid | JWT signature verifies against Google's public keys |
| Issuer Correct | `iss` is `https://cloud.google.com/iap` |
| Token Not Expired | `exp` is in the future |
| Email Consistent | Email header matches JWT `email` claim |
| No Bypass Risk | No unsigned email/ID headers without JWT |

---

## Integrating IAP in Your Own App

Once IAP is configured on your Cloud Run service, your application code just needs to verify the JWT and extract the user identity. Here are minimal examples in several languages.

### Minimal Go Example

```go
package main

import (
    "fmt"
    "log"
    "net/http"
    "os"

    "google.golang.org/api/idtoken"
)

func main() {
    audience := os.Getenv("IAP_AUDIENCE")
    if audience == "" {
        log.Fatal("IAP_AUDIENCE environment variable is required")
    }

    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        jwt := r.Header.Get("X-Goog-IAP-JWT-Assertion")
        if jwt == "" {
            http.Error(w, "No IAP JWT", http.StatusUnauthorized)
            return
        }

        payload, err := idtoken.Validate(r.Context(), jwt, audience)
        if err != nil {
            http.Error(w, "Invalid JWT: "+err.Error(), http.StatusForbidden)
            return
        }

        email := payload.Claims["email"].(string)
        fmt.Fprintf(w, "Hello, %s!", email)
    })

    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    log.Fatal(http.ListenAndServe(":"+port, nil))
}
```

```
go get google.golang.org/api/idtoken
```

### Python / Flask Example

```python
import os
from flask import Flask, request, abort
from google.auth.transport import requests as google_requests
from google.oauth2 import id_token

app = Flask(__name__)
AUDIENCE = os.environ["IAP_AUDIENCE"]

@app.route("/")
def index():
    jwt_token = request.headers.get("X-Goog-IAP-JWT-Assertion")
    if not jwt_token:
        abort(401, "No IAP JWT")

    try:
        claims = id_token.verify_token(
            jwt_token,
            google_requests.Request(),
            audience=AUDIENCE,
            certs_url="https://www.gstatic.com/iap/verify/public_key-jwk",
        )
    except Exception as e:
        abort(403, f"Invalid JWT: {e}")

    return f"Hello, {claims['email']}!"

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=int(os.environ.get("PORT", 8080)))
```

```
pip install flask google-auth requests
```

### Node.js / Express Example

```javascript
const express = require("express");
const { OAuth2Client } = require("google-auth-library");

const app = express();
const audience = process.env.IAP_AUDIENCE;
const client = new OAuth2Client();

if (!audience) {
  console.error("IAP_AUDIENCE environment variable is required");
  process.exit(1);
}

app.get("/", async (req, res) => {
  const jwt = req.headers["x-goog-iap-jwt-assertion"];
  if (!jwt) {
    return res.status(401).send("No IAP JWT");
  }

  try {
    const ticket = await client.verifySignedJwtWithCertsAsync(
      jwt,
      "https://www.gstatic.com/iap/verify/public_key-jwk",
      audience,
      ["https://cloud.google.com/iap"]
    );
    const email = ticket.getPayload().email;
    res.send(`Hello, ${email}!`);
  } catch (err) {
    res.status(403).send(`Invalid JWT: ${err.message}`);
  }
});

const port = process.env.PORT || 8080;
app.listen(port, () => console.log(`Listening on port ${port}`));
```

```
npm install express google-auth-library
```

---

## Troubleshooting

### "audience provided does not match aud claim in the JWT"

Your `IAP_AUDIENCE` env var doesn't match the `aud` claim in the JWT.

**Fix:** The correct format for Cloud Run native IAP is:
```
/projects/{PROJECT_NUMBER}/locations/{REGION}/services/{SERVICE_NAME}
```

Use the RunIAP JWT page or decode the JWT manually to see the actual `aud` value, then update:
```bash
gcloud run services update myapp --region=us-central1 \
  --update-env-vars=IAP_AUDIENCE=/projects/123456/locations/us-central1/services/myapp
```

### Users get 403 after authenticating

The IAP service agent likely doesn't have the invoker role:
```bash
PROJECT_NUMBER=$(gcloud projects describe $(gcloud config get-value project) --format='value(projectNumber)')
gcloud run services add-iam-policy-binding myapp \
  --region=us-central1 \
  --member=serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com \
  --role=roles/run.invoker
```

### IAP headers not appearing

- Verify IAP is enabled: `gcloud run services describe myapp --region=us-central1` should show `run.googleapis.com/iap-enabled: 'true'`
- Verify the service uses `--no-allow-unauthenticated`
- IAP headers only appear when the request goes through IAP. Direct `curl` calls to the Cloud Run URL won't have them.

### JWT verification works but email header is missing

This is normal — the JWT is the authoritative source. The convenience headers (`X-Goog-Authenticated-User-Email`, `X-Goog-Authenticated-User-ID`) should not be relied upon. Read the email from the verified JWT claims instead.

---

## Architecture

The IAP app lives in `cmd/runiap/` within the [cloud-run-auth monorepo](../README.md):

```
cmd/runiap/
└── main.go                          # HTTP server, routing, IAP middleware

deploy/
├── _common.sh                       # Shared variables + functions
├── setup-iap.sh                     # One-time IAP infrastructure setup
└── deploy-iap.sh                    # Build and deploy with IAP

internal/
├── iap/
│   ├── claims.go                    # IAP header constants, Claims struct
│   ├── detect.go                    # Header detection + spoofing warning
│   ├── context.go                   # Request context helpers
│   └── verify.go                    # JWT signature verification via idtoken
├── handler/iaphandler/
│   ├── dashboard.go                 # Main status page
│   ├── headers.go                   # Raw header inspector
│   ├── jwt.go                       # JWT decoder/verifier UI
│   ├── audience.go                  # Audience comparison tool
│   ├── diagnostic.go                # Automated health checks
│   ├── log.go                       # Request timeline
│   └── healthz.go                   # Health check endpoint
├── components/iapui/
│   ├── dashboard.templ              # Dashboard UI
│   ├── headers.templ                # Headers table
│   ├── jwt.templ                    # JWT panels
│   ├── audience.templ               # Audience form
│   ├── diagnostic.templ             # Check results
│   ├── log.templ                    # Request log table
│   └── types.go                     # IAP-specific template data types
└── shared/                          # Shared with other apps
    ├── middleware.go                 # Logging + request log middleware
    ├── components/layout.templ      # Parameterized page layout
    ├── render/render.go             # Content negotiation (HTML/JSON)
    └── reqlog/ringbuffer.go         # Thread-safe circular buffer (200 entries)
```

**Key dependencies:**
- [`google.golang.org/api/idtoken`](https://pkg.go.dev/google.golang.org/api/idtoken) — JWT signature verification
- [`github.com/a-h/templ`](https://templ.guide/) — Type-safe HTML templating
