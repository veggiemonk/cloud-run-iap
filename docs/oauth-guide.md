# OAuth on Cloud Run — In-Depth Guide

> This guide covers deploying Cloud Run services with OAuth 2.0 (authorization code flow) — how OAuth works, deployment, token management, session handling, GCP API access, security pitfalls, troubleshooting, and integration examples in Go, Python, and Node.js.
>
> For the project overview and auth model comparison, see the [top-level README](../README.md). For the IAP alternative, see the [IAP guide](iap-guide.md).
>
> **App location:** [`cmd/runoauth/`](../cmd/runoauth/)

**Official docs:**
- [OAuth 2.0 for Web Server Applications](https://developers.google.com/identity/protocols/oauth2/web-server)
- [Google OAuth 2.0 Scopes](https://developers.google.com/identity/protocols/oauth2/scopes)
- [OAuth 2.0 Playground](https://developers.google.com/oauthplayground/)

---

## Table of Contents

- [Quick Start](#quick-start)
- [How OAuth Works on Cloud Run](#how-oauth-works-on-cloud-run)
- [IAP vs OAuth — When to Use Which](#iap-vs-oauth--when-to-use-which)
- [Deployment Guide](#deployment-guide)
  - [Prerequisites](#prerequisites)
  - [Creating OAuth Credentials](#creating-oauth-credentials)
  - [Deploy with OAuth](#deploy-with-oauth)
  - [Step-by-Step Manual Deployment](#step-by-step-manual-deployment)
- [Understanding the OAuth Flow](#understanding-the-oauth-flow)
  - [Authorization Code Flow](#authorization-code-flow)
  - [State Parameter (CSRF Protection)](#state-parameter-csrf-protection)
  - [Token Exchange](#token-exchange)
- [Tokens and Scopes](#tokens-and-scopes)
  - [Access Tokens vs Refresh Tokens](#access-tokens-vs-refresh-tokens)
  - [Scopes](#scopes)
  - [Token Lifecycle](#token-lifecycle)
- [Session Management](#session-management)
  - [Why You Need Sessions](#why-you-need-sessions)
  - [RunOAuth's Session Architecture](#runoauths-session-architecture)
  - [Session Security](#session-security)
- [Accessing GCP APIs](#accessing-gcp-apis)
  - [Using the User's Token](#using-the-users-token)
  - [What RunOAuth Demonstrates](#what-runoauth-demonstrates)
  - [Go Implementation](#go-implementation)
- [Common Security Mistakes](#common-security-mistakes)
- [RunOAuth Diagnostic Pages](#runoauth-diagnostic-pages)
- [Integrating OAuth in Your Own App](#integrating-oauth-in-your-own-app)
  - [Minimal Go Example](#minimal-go-example)
  - [Python / Flask Example](#python--flask-example)
  - [Node.js / Express Example](#nodejs--express-example)
- [Troubleshooting](#troubleshooting)
- [Architecture](#architecture)

---

## Quick Start

```bash
./cmd/runoauth/deploy-oauth.sh runoauth --secret-name internal-tools-google-oauth-secret
```

The script generates templ code, builds with [ko](https://ko.build/), pushes to Artifact Registry, deploys to Cloud Run, reads OAuth credentials from Secret Manager (or prompts interactively), and prints the redirect URI to add to your OAuth credentials.

> **Note:** You must first create OAuth credentials in the [Google Cloud Console](https://console.cloud.google.com/apis/credentials). You can store them in Secret Manager for automated deploys, or the script will prompt interactively.

---

## How OAuth Works on Cloud Run

```
┌──────────┐     ┌──────────────────┐     ┌──────────────────┐
│  Browser  │────▶│  Cloud Run       │────▶│  Google OAuth    │
│           │◀────│  Service         │◀────│  Server          │
└──────────┘     └──────────────────┘     └──────────────────┘
                   │                         │
                   │ 1. User visits app      │ 4. User consents
                   │ 2. App redirects to     │ 5. Google redirects
                   │    Google OAuth          │    back with auth code
                   │ 3. Google shows          │ 6. App exchanges code
                   │    consent screen        │    for access token
                   │                         │
                   ▼                         ▼
              App manages auth          Google provides tokens
              and sessions              that call GCP APIs
```

With OAuth on Cloud Run:

1. **Your app** is publicly reachable (`--allow-unauthenticated`) — it handles auth itself.
2. **Unauthenticated users** are redirected to Google's OAuth consent screen by your app's middleware.
3. **After consent**, Google redirects back to your app with an authorization code.
4. **Your app** exchanges the code for an access token (and optionally a refresh token).
5. **The access token** can call GCP APIs on behalf of the user — this is the key difference from IAP.

> **Critical difference from IAP:** With IAP, `--allow-unauthenticated` is a security mistake that bypasses all protection. With OAuth, `--allow-unauthenticated` is *required* because users must reach your app's login page before they can authenticate. Your app enforces auth in its own middleware.

---

## IAP vs OAuth — When to Use Which

| Need | Use |
|------|-----|
| Gate access to an internal tool | **IAP** — simpler, no code changes needed |
| Know who the user is | **Either** — both provide user identity |
| Access GCP APIs as the user | **OAuth** — only OAuth provides access tokens |
| Avoid managing sessions | **IAP** — Google manages sessions for you |
| Work without Google Cloud IAP | **OAuth** — works with any OAuth provider |

**Rule of thumb:** If you just need to know *who* the user is, use IAP. If you need to *act as* the user (read their Drive files, list their GCP projects, query their BigQuery datasets), use OAuth.

---

## Deployment Guide

### Prerequisites

- [Google Cloud SDK (`gcloud`)](https://cloud.google.com/sdk/docs/install) installed and authenticated
- A GCP project with billing enabled
- OAuth 2.0 credentials (Client ID + Client Secret) — [see below](#creating-oauth-credentials)
- [ko](https://ko.build/) installed (`go install github.com/google/ko@latest`)
- [`jq`](https://jqlang.github.io/jq/) installed (only needed with `--secret-name` for Secret Manager)
- Docker is **not** required locally — ko builds Go images directly

### Creating OAuth Credentials

Before deploying, you need OAuth 2.0 credentials:

1. Go to the [Google Cloud Console — Credentials](https://console.cloud.google.com/apis/credentials)
2. Click **Create Credentials** → **OAuth client ID**
3. Application type: **Web application**
4. Name: anything (e.g., "RunOAuth")
5. Leave the redirect URIs empty for now — you'll add it after deploying
6. Click **Create** and copy the **Client ID** and **Client Secret**

> **First-time setup:** If you haven't configured the OAuth consent screen yet, Google will prompt you. Choose **Internal** (for Google Workspace) or **External** (for any Google account). For testing, **External** in "Testing" mode works fine — you'll need to add test users manually.

**Optional: Store credentials in Secret Manager** for automated deploys:

```bash
# Download the OAuth client JSON from the Credentials page, then:
gcloud secrets create my-oauth-secret --data-file=client_secret_*.json
```

Then use `--secret-name my-oauth-secret` when deploying instead of entering credentials interactively.

### Deploy with OAuth

```bash
./cmd/runoauth/deploy-oauth.sh <service-name> [--region <region>] [--secret-name <name>]
```

**Examples:**

```bash
# Deploy with defaults (us-central1), prompt for credentials
./cmd/runoauth/deploy-oauth.sh myapp

# Deploy with credentials from Secret Manager
./cmd/runoauth/deploy-oauth.sh myapp --secret-name internal-tools-google-oauth-secret

# Deploy to a specific region
./cmd/runoauth/deploy-oauth.sh myapp --region europe-west1
```

**What the script does (5 steps):**

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `gcloud services enable ...` | Enable Cloud Run, Artifact Registry, Secret Manager APIs |
| 2 | Secret Manager read or interactive prompt | Get OAuth Client ID and Client Secret |
| 3 | `templ generate` + `ko build` | Generate templ code, build Go image with ko, push to Artifact Registry |
| 4 | `gcloud run deploy --image ... --set-env-vars ...` | Deploy ko-built image with env vars (app handles auth) |
| 5 | Verify URL | If the actual service URL differs from predicted, update `OAUTH_REDIRECT_URL` |

After deployment, the script prints the redirect URI. You **must** add it to your OAuth credentials in the Console, or the callback will fail.

> **Secret Manager format:** When using `--secret-name`, the secret must contain the OAuth client JSON downloaded from the Google Cloud Console: `{"web": {"client_id": "...", "client_secret": "..."}}`. The script uses `jq` to parse it.

### Step-by-Step Manual Deployment

#### 1. Enable APIs

```bash
gcloud services enable \
  run.googleapis.com \
  artifactregistry.googleapis.com
```

#### 2. Build and push the image with ko

```bash
# Generate templ code first
go tool templ generate

# Build and push (set KO_DOCKER_REPO to your Artifact Registry including image name)
PROJECT_ID=$(gcloud config get-value project)
export KO_DOCKER_REPO="us-central1-docker.pkg.dev/${PROJECT_ID}/cloud-run-source-deploy/runoauth"
ko build ./cmd/runoauth --bare --platform=linux/amd64
```

#### 3. Deploy the image with environment variables

```bash
IMAGE="${KO_DOCKER_REPO}"
SERVICE_URL="https://myapp-$(gcloud projects describe $(gcloud config get-value project) --format='value(projectNumber)').us-central1.run.app"

gcloud run deploy myapp \
  --image="$IMAGE" \
  --region=us-central1 \
  --allow-unauthenticated \
  --port=8080 \
  --set-env-vars="GOOGLE_CLIENT_ID=xxx,GOOGLE_CLIENT_SECRET=yyy,OAUTH_REDIRECT_URL=${SERVICE_URL}/auth/callback"
```

Key flags:
- `--image` — deploy the ko-built image from Artifact Registry
- `--allow-unauthenticated` — **required** for OAuth apps (the app handles auth itself)
- `--set-env-vars` — set OAuth credentials and redirect URL in the same deploy to avoid startup failures

#### 4. Get the redirect URI

```bash
SERVICE_URL=$(gcloud run services describe myapp --region=us-central1 --format='value(status.url)')
echo "Redirect URI: ${SERVICE_URL}/auth/callback"
```

#### 5. Add the redirect URI to your OAuth credentials

Go to the [Credentials page](https://console.cloud.google.com/apis/credentials), edit your OAuth client, and add `${SERVICE_URL}/auth/callback` as an authorized redirect URI.

---

## Understanding the OAuth Flow

### Authorization Code Flow

RunOAuth implements the [OAuth 2.0 authorization code flow](https://datatracker.ietf.org/doc/html/rfc6749#section-4.1), the standard for server-side web applications:

```
Browser                 App                    Google
  │                      │                       │
  │  GET /               │                       │
  │─────────────────────▶│                       │
  │  302 → /auth/login   │                       │
  │◀─────────────────────│                       │
  │                      │                       │
  │  GET /auth/login     │                       │
  │─────────────────────▶│                       │
  │  Set state cookie    │                       │
  │  302 → Google        │                       │
  │◀─────────────────────│                       │
  │                      │                       │
  │  Google consent      │                       │
  │─────────────────────────────────────────────▶│
  │  302 → /auth/callback?code=xxx&state=yyy     │
  │◀─────────────────────────────────────────────│
  │                      │                       │
  │  GET /auth/callback  │                       │
  │─────────────────────▶│                       │
  │                      │  Exchange code for    │
  │                      │  token (server-side)  │
  │                      │──────────────────────▶│
  │                      │  Access token +       │
  │                      │  refresh token        │
  │                      │◀──────────────────────│
  │                      │                       │
  │                      │  Fetch userinfo       │
  │                      │──────────────────────▶│
  │                      │  Email, name, picture │
  │                      │◀──────────────────────│
  │                      │                       │
  │  Set session cookie  │                       │
  │  302 → /             │                       │
  │◀─────────────────────│                       │
```

The authorization code is exchanged server-side — it never touches the browser after the redirect. This is why the authorization code flow is more secure than the implicit flow (which exposes tokens in the URL fragment).

### State Parameter (CSRF Protection)

The `state` parameter prevents [cross-site request forgery (CSRF)](https://datatracker.ietf.org/doc/html/rfc6749#section-10.12) attacks on the OAuth callback:

1. Before redirecting to Google, the app generates a 32-byte random state and stores it in a cookie
2. The state is included in the authorization URL
3. Google passes it back in the callback
4. The app verifies the callback state matches the cookie

Without this, an attacker could craft a URL that completes the OAuth flow with *their* authorization code, potentially linking the victim's session to the attacker's Google account.

```go
// Generate state (32 bytes of crypto/rand, hex-encoded)
state := generateState()

// Store in cookie
http.SetCookie(w, &http.Cookie{
    Name:     "oauth_state",
    Value:    state,
    HttpOnly: true,
    SameSite: http.SameSiteLaxMode,
    MaxAge:   600, // 10 minutes
})

// Include in auth URL
url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
```

### Token Exchange

The code-for-token exchange happens server-to-server — your app sends its client secret to Google's token endpoint. The browser never sees the access token.

```go
token, err := cfg.Exchange(r.Context(), code)
// token.AccessToken — bearer token for GCP API calls
// token.RefreshToken — long-lived token to get new access tokens
// token.Expiry — when the access token expires (typically 1 hour)
```

---

## Tokens and Scopes

### Access Tokens vs Refresh Tokens

| Token | Lifetime | Purpose | Security risk |
|-------|----------|---------|---------------|
| **Access token** | ~1 hour | Authorize API calls | If leaked, attacker can act as user until it expires |
| **Refresh token** | Long-lived (months) | Obtain new access tokens | If leaked, attacker has persistent access until revoked |

Access tokens are short-lived and automatically expire. Refresh tokens persist across restarts and can obtain new access tokens without user interaction.

RunOAuth requests `oauth2.AccessTypeOffline` to obtain a refresh token, which means the app can continue making API calls even after the access token expires.

### Scopes

RunOAuth requests these scopes:

| Scope | Purpose |
|-------|---------|
| `openid` | OpenID Connect — enables ID token |
| `email` | Access the user's email address |
| `profile` | Access the user's name and profile picture |
| `cloud-platform.read-only` | Read-only access to all GCP resources the user can see |

**Scope security:** The `cloud-platform.read-only` scope is deliberately broad to demonstrate the power (and danger) of OAuth. In production, request only the specific scopes you need:

```go
// BROAD — gives access to everything (for demos only)
"https://www.googleapis.com/auth/cloud-platform.read-only"

// NARROW — only what you need (for production)
"https://www.googleapis.com/auth/cloudresourcemanager.projects.get"
"https://www.googleapis.com/auth/bigquery.readonly"
```

### Token Lifecycle

```
User consents     Access token    Access token    Refresh token
to scopes    ──▶  issued     ──▶  expires (1h) ──▶  used to get
                                                    new access token
                                                        │
                                                        ▼
                                                   New access token
                                                   (repeat until
                                                    revoked)
```

Google may return a refresh token only on the first authorization. Subsequent logins may not include one unless you add `oauth2.ApprovalForce` (which re-shows the consent screen).

---

## Session Management

### Why You Need Sessions

Unlike IAP (where Google manages sessions automatically), OAuth apps must manage sessions themselves. After the OAuth callback, you have a token — but you need a way to associate subsequent requests with that token without re-running the OAuth flow every time.

### RunOAuth's Session Architecture

```
Browser                          App Server
  │                                │
  │  Cookie: session_id=abc123     │
  │───────────────────────────────▶│
  │                                │  Lookup abc123 in SessionStore
  │                                │  → { Email, Name, Token, ... }
  │                                │
  │                                │  Use Token to call GCP APIs
  │                                │
```

RunOAuth uses in-memory sessions:

- **Session ID**: 32 bytes from `crypto/rand`, hex-encoded (64 characters)
- **Cookie**: `session_id`, `HttpOnly`, `SameSite=Lax`
- **Storage**: `sync.RWMutex`-protected `map[string]*Session`
- **Contents**: user email, name, picture, OAuth token, creation time

> **Production note:** In-memory sessions are lost on container restart. For production, use a persistent store (Redis, Cloud Firestore, encrypted cookies). Cloud Run can scale to multiple instances, each with its own memory — in-memory sessions won't work with `--max-instances > 1` unless you enable [session affinity](https://cloud.google.com/run/docs/configuring/session-affinity).

### Session Security

| Concern | Mitigation |
|---------|-----------|
| Session hijacking | `HttpOnly` cookie prevents JavaScript access |
| CSRF on login | State parameter validated on callback |
| Session fixation | New session ID generated after login |
| Token in browser | Token stored server-side, never in cookies |
| Brute-force session IDs | 256-bit random IDs (2^256 keyspace) |

---

## Accessing GCP APIs

### Using the User's Token

The key advantage of OAuth over IAP: your app can call GCP APIs **as the user**. The access token authorizes API calls with the user's permissions.

```
App receives       App creates          App calls GCP API
user's token  ──▶  API client with  ──▶  on behalf of user
                   user's token
```

This means your app can read the user's GCP projects, query their BigQuery datasets, list their Cloud Storage buckets — anything the user has access to, within the scopes they consented to.

### What RunOAuth Demonstrates

The GCP Explorer page uses the user's access token to list their GCP projects via the [Cloud Resource Manager API](https://cloud.google.com/resource-manager/reference/rest):

```
User's token + cloud-platform.read-only scope
    │
    ▼
cloudresourcemanager.projects.search()
    │
    ▼
List of projects the user can see
(project ID, display name)
```

The security warning on this page is intentional: *"Your app could silently exfiltrate this data."* This demonstrates that OAuth tokens are **ambient authority** — once your app has the token, it can do anything the token allows, silently.

### Go Implementation

RunOAuth uses the user's token to create an authenticated GCP client:

```go
import (
    "golang.org/x/oauth2"
    "google.golang.org/api/cloudresourcemanager/v3"
    "google.golang.org/api/option"
)

func listProjects(ctx context.Context, userToken *oauth2.Token) ([]*cloudresourcemanager.Project, error) {
    // Create a token source from the user's OAuth token
    ts := oauth2.StaticTokenSource(userToken)

    // Create the Cloud Resource Manager client with the user's credentials
    svc, err := cloudresourcemanager.NewService(ctx, option.WithTokenSource(ts))
    if err != nil {
        return nil, err
    }

    // List projects — this runs with the user's permissions
    resp, err := svc.Projects.Search().Do()
    if err != nil {
        return nil, err
    }
    return resp.Projects, nil
}
```

`option.WithTokenSource(ts)` tells the API client to authenticate using the user's access token instead of the service account's default credentials. Every API call made through this client acts as the user.

---

## Common Security Mistakes

### 1. Storing tokens in cookies or URLs

```go
// WRONG — tokens in cookies are exposed to JavaScript (if not HttpOnly)
// and to anyone who can read cookies
http.SetCookie(w, &http.Cookie{
    Name:  "access_token",
    Value: token.AccessToken,
})
```

```go
// CORRECT — store only a session ID in the cookie; keep the token server-side
session := sessions.Create(email, name, picture, token)
http.SetCookie(w, &http.Cookie{
    Name:     "session_id",
    Value:    session.ID,
    HttpOnly: true,
    SameSite: http.SameSiteLaxMode,
})
```

### 2. Skipping the state parameter

Without CSRF protection, an attacker can complete the OAuth flow with their own authorization code:

```go
// WRONG — no state parameter
url := cfg.AuthCodeURL("")

// CORRECT — generate and validate state
state := generateState()
http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, ...})
url := cfg.AuthCodeURL(state)
// In callback: verify r.URL.Query().Get("state") == cookie value
```

### 3. Over-granting scopes

Requesting `cloud-platform` (full read-write) when you only need `cloud-platform.read-only` (or even narrower scopes) gives your app — and any attacker who compromises it — far more power than necessary.

```go
// WRONG — full read-write access to everything
Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"}

// CORRECT — minimum necessary access
Scopes: []string{"https://www.googleapis.com/auth/cloudresourcemanager.projects.get"}
```

### 4. Using `--no-allow-unauthenticated` with OAuth

With OAuth, your app handles authentication. If you deploy with `--no-allow-unauthenticated`, users can't reach your login page — they'll get a 403 from Cloud Run before your app even sees the request.

```bash
# WRONG — users can't reach the login page
gcloud run deploy myapp --no-allow-unauthenticated

# CORRECT — app handles auth itself
gcloud run deploy myapp --allow-unauthenticated
```

> **Contrast with IAP:** IAP requires `--no-allow-unauthenticated`. This is one of the most confusing differences between the two models.

### 5. Not validating the redirect URI

Always configure `OAUTH_REDIRECT_URL` explicitly. If your app constructs the redirect URI from the `Host` header, an attacker could manipulate it to redirect the authorization code to their server.

```go
// WRONG — constructing redirect from request
redirectURL := "https://" + r.Host + "/auth/callback"

// CORRECT — using a configured, fixed redirect URL
redirectURL := os.Getenv("OAUTH_REDIRECT_URL")
```

### 6. Logging tokens

Access tokens and refresh tokens are credentials. Logging them creates a persistent record that could be exploited.

```go
// WRONG
slog.Info("user authenticated", "token", token.AccessToken)

// CORRECT
slog.Info("user authenticated", "email", userInfo.Email)
```

### 7. Using the implicit flow

The implicit flow (`response_type=token`) returns the access token in the URL fragment, which is visible in browser history and referrer headers. Always use the authorization code flow (`response_type=code`) for server-side apps.

### 8. Treating access tokens as identity

Access tokens authorize API calls — they don't reliably identify users. Always fetch userinfo or use an ID token to establish identity.

```go
// WRONG — inferring identity from what the token can do
// (also, this can change if the token is refreshed or scopes change)

// CORRECT — fetch user identity from the userinfo endpoint
resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
```

---

## RunOAuth Diagnostic Pages

RunOAuth provides four pages for understanding and debugging OAuth:

| Page | Path | Purpose |
|------|------|---------|
| **Dashboard** | `/` | At-a-glance status: user identity, token expiry, session age |
| **Token** | `/token` | Masked access token, refresh token presence, scopes, expiry |
| **GCP Explorer** | `/gcp` | List the user's GCP projects using their access token |
| **Diagnostic** | `/diagnostic` | Automated health checks: session, token expiry, scopes, GCP API reachability |

Every page supports JSON output by appending `?format=json` or setting `Accept: application/json`.

Auth routes (no login required):

| Route | Purpose |
|-------|---------|
| `GET /auth/login` | Redirect to Google OAuth consent |
| `GET /auth/callback` | Handle OAuth redirect, create session |
| `GET /auth/logout` | Destroy session, redirect to login |
| `GET /healthz` | Health check (`{"status":"ok"}`) |

The **Diagnostic** page runs these checks:

| Check | Pass condition |
|-------|----------------|
| Session Valid | User email present in session |
| Token Expiry | Access token not expired |
| Scopes | Scope information available in token response |
| GCP API Reachable | Can successfully call Cloud Resource Manager API |

Each page includes a security pitfall callout relevant to what it displays — making RunOAuth a teaching tool, not just a demo.

---

## Integrating OAuth in Your Own App

### Minimal Go Example

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"

    "golang.org/x/oauth2"
    "golang.org/x/oauth2/google"
)

var oauthConfig = &oauth2.Config{
    ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
    ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
    RedirectURL:  os.Getenv("OAUTH_REDIRECT_URL"),
    Scopes:       []string{"openid", "email", "profile"},
    Endpoint:     google.Endpoint,
}

func main() {
    http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
        url := oauthConfig.AuthCodeURL("state-todo-use-random")
        http.Redirect(w, r, url, http.StatusFound)
    })

    http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
        code := r.URL.Query().Get("code")
        token, err := oauthConfig.Exchange(r.Context(), code)
        if err != nil {
            http.Error(w, err.Error(), 500)
            return
        }

        client := oauthConfig.Client(r.Context(), token)
        resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
        if err != nil {
            http.Error(w, err.Error(), 500)
            return
        }
        defer resp.Body.Close()

        var info struct{ Email string }
        json.NewDecoder(resp.Body).Decode(&info)
        fmt.Fprintf(w, "Hello, %s!", info.Email)
    })

    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    log.Fatal(http.ListenAndServe(":"+port, nil))
}
```

```
go get golang.org/x/oauth2
```

> **Note:** This minimal example omits state validation, session management, and error handling. See RunOAuth's source for a complete implementation.

### Python / Flask Example

```python
import os
import requests
from flask import Flask, redirect, request, session, url_for
from authlib.integrations.flask_client import OAuth

app = Flask(__name__)
app.secret_key = os.urandom(32)

oauth = OAuth(app)
google = oauth.register(
    name="google",
    client_id=os.environ["GOOGLE_CLIENT_ID"],
    client_secret=os.environ["GOOGLE_CLIENT_SECRET"],
    server_metadata_url="https://accounts.google.com/.well-known/openid-configuration",
    client_kwargs={"scope": "openid email profile"},
)

@app.route("/login")
def login():
    redirect_uri = os.environ["OAUTH_REDIRECT_URL"]
    return google.authorize_redirect(redirect_uri)

@app.route("/callback")
def callback():
    token = google.authorize_access_token()
    userinfo = token.get("userinfo")
    session["email"] = userinfo["email"]
    return redirect("/")

@app.route("/")
def index():
    email = session.get("email")
    if not email:
        return redirect("/login")
    return f"Hello, {email}!"

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=int(os.environ.get("PORT", 8080)))
```

```
pip install flask authlib requests
```

### Node.js / Express Example

```javascript
const express = require("express");
const { OAuth2Client } = require("google-auth-library");
const session = require("express-session");

const app = express();
app.use(session({
  secret: require("crypto").randomBytes(32).toString("hex"),
  resave: false,
  saveUninitialized: false,
}));

const client = new OAuth2Client(
  process.env.GOOGLE_CLIENT_ID,
  process.env.GOOGLE_CLIENT_SECRET,
  process.env.OAUTH_REDIRECT_URL
);

app.get("/login", (req, res) => {
  const url = client.generateAuthUrl({
    access_type: "offline",
    scope: ["openid", "email", "profile"],
  });
  res.redirect(url);
});

app.get("/callback", async (req, res) => {
  const { tokens } = await client.getToken(req.query.code);
  client.setCredentials(tokens);

  const userinfo = await client.request({
    url: "https://www.googleapis.com/oauth2/v2/userinfo",
  });
  req.session.email = userinfo.data.email;
  res.redirect("/");
});

app.get("/", (req, res) => {
  if (!req.session.email) return res.redirect("/login");
  res.send(`Hello, ${req.session.email}!`);
});

const port = process.env.PORT || 8080;
app.listen(port, () => console.log(`Listening on port ${port}`));
```

```
npm install express google-auth-library express-session
```

---

## Troubleshooting

### "redirect_uri_mismatch" error

The redirect URI in your OAuth credentials doesn't match what the app sends.

**Fix:** Go to [Credentials](https://console.cloud.google.com/apis/credentials), edit your OAuth client, and add the exact redirect URI. It must match exactly, including the scheme (`https://`), host, and path (`/auth/callback`).

```bash
# Find your service URL
SERVICE_URL=$(gcloud run services describe myapp --region=us-central1 --format='value(status.url)')
echo "Add this redirect URI: ${SERVICE_URL}/auth/callback"
```

### "Access blocked: app has not been verified"

Your OAuth consent screen is in "Testing" mode and the user isn't in the test users list.

**Fix:** Go to [OAuth consent screen](https://console.cloud.google.com/apis/credentials/consent), add the user's email to the test users list, or publish the app (which requires Google's review for sensitive scopes).

### Token expires after 1 hour

This is normal. Access tokens are short-lived by design.

**Fix:** Request a refresh token by using `oauth2.AccessTypeOffline`:

```go
url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
```

Then use the refresh token to obtain new access tokens:

```go
// The oauth2 package handles refresh automatically when you use
// cfg.Client(ctx, token) — it refreshes when the access token expires.
client := cfg.Client(ctx, token)
```

### "insufficient_scope" when calling GCP APIs

The user consented to fewer scopes than your app needs, or you're calling an API that requires a scope you didn't request.

**Fix:** Add the necessary scope to your config and have the user re-authorize:

```go
Scopes: []string{
    "openid", "email", "profile",
    "https://www.googleapis.com/auth/cloud-platform.read-only",
}
```

### Session lost after container restart

In-memory sessions don't survive container restarts.

**Fix:** Use a persistent session store:
- **Redis** via [Cloud Memorystore](https://cloud.google.com/memorystore)
- **Cloud Firestore** for serverless persistence
- **Encrypted cookies** (stateless — limited by cookie size)

### Users see 403 before reaching the login page

You likely deployed with `--no-allow-unauthenticated`. OAuth apps must be publicly reachable.

```bash
# Fix: allow unauthenticated access
gcloud run services add-iam-policy-binding myapp \
  --region=us-central1 \
  --member=allUsers \
  --role=roles/run.invoker
```

---

## Architecture

The OAuth app lives in `cmd/runoauth/` within the [cloud-run-auth monorepo](../README.md):

```
cmd/runoauth/
├── main.go                          # HTTP server, routing, OAuth wiring
├── Dockerfile                       # Multi-stage build (Go build → distroless)
└── deploy-oauth.sh                  # Full deploy-with-OAuth script

internal/
├── oauth/
│   ├── google.go                    # OAuth config, login/callback/logout handlers
│   ├── session.go                   # In-memory session store
│   └── middleware.go                # RequireAuth middleware, context helpers
├── handler/oauthhandler/
│   ├── dashboard.go                 # User identity page
│   ├── token.go                     # Token inspector
│   ├── gcp.go                       # GCP project explorer
│   ├── diagnostic.go                # Automated health checks
│   └── healthz.go                   # Health check endpoint
├── components/oauthui/
│   ├── dashboard.templ              # Dashboard UI with security warnings
│   ├── token.templ                  # Token details with leakage warning
│   ├── gcp.templ                    # Project list with exfiltration warning
│   ├── diagnostic.templ             # Check results
│   └── types.go                     # OAuth-specific template data types
└── shared/                          # Shared with other apps
    ├── middleware.go                 # Logging + request log middleware
    ├── components/layout.templ      # Parameterized page layout
    ├── render/render.go             # Content negotiation (HTML/JSON)
    └── reqlog/ringbuffer.go         # Thread-safe circular buffer (200 entries)
```

**Key dependencies:**
- [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) — OAuth 2.0 flow
- [`google.golang.org/api/cloudresourcemanager/v3`](https://pkg.go.dev/google.golang.org/api/cloudresourcemanager/v3) — GCP project listing
- [`github.com/a-h/templ`](https://templ.guide/) — Type-safe HTML templating
- `gcr.io/distroless/static-debian12:nonroot` — Minimal, secure runtime image
