#!/usr/bin/env bash
set -euo pipefail

# Deploy a Cloud Run service with OAuth using ko (no Dockerfile needed)
#
# Usage:
#   ./deploy-oauth.sh <service-name> [--region <region>] [--secret-name <name>]
#
# Examples:
#   ./deploy-oauth.sh runoauth
#   ./deploy-oauth.sh runoauth --secret-name internal-tools-google-oauth-secret

REGION="us-central1"
SECRET_NAME=""

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <service-name> [--region <region>] [--secret-name <name>]"
  exit 1
fi

SERVICE_NAME="$1"
shift

while [[ $# -gt 0 ]]; do
  case "$1" in
    --region)      REGION="$2";      shift 2 ;;
    --secret-name) SECRET_NAME="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

PROJECT_ID=$(gcloud config get-value project 2>/dev/null)

echo "=== Deploying $SERVICE_NAME to Cloud Run with OAuth (via ko) ==="
echo "  Project:  $PROJECT_ID"
echo "  Region:   $REGION"
echo ""

# --- Step 1: Enable required APIs ---
echo ">>> Step 1: Enabling required APIs..."
gcloud services enable \
  run.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com

# --- Step 2: Get OAuth credentials ---
echo ""
echo ">>> Step 2: OAuth credentials setup"

if [[ -n "$SECRET_NAME" ]]; then
  echo "  Reading credentials from Secret Manager: $SECRET_NAME"
  SECRET_JSON=$(gcloud secrets versions access latest --secret="$SECRET_NAME" 2>/dev/null) || {
    echo "Error: Could not read secret '$SECRET_NAME' from Secret Manager"
    exit 1
  }
  CLIENT_ID=$(echo "$SECRET_JSON" | jq -r '.web.client_id')
  CLIENT_SECRET=$(echo "$SECRET_JSON" | jq -r '.web.client_secret')
  if [[ -z "$CLIENT_ID" || "$CLIENT_ID" == "null" || -z "$CLIENT_SECRET" || "$CLIENT_SECRET" == "null" ]]; then
    echo "Error: Could not parse client_id or client_secret from secret JSON"
    echo "  Expected format: {\"web\": {\"client_id\": \"...\", \"client_secret\": \"...\"}}"
    exit 1
  fi
  echo "  Client ID: ${CLIENT_ID:0:20}..."
else
  echo ""
  echo "  You must create OAuth 2.0 credentials in the Google Cloud Console:"
  echo "  1. Go to: https://console.cloud.google.com/apis/credentials"
  echo "  2. Click 'Create Credentials' → 'OAuth client ID'"
  echo "  3. Application type: 'Web application'"
  echo "  4. Add authorized redirect URI (will be shown after deploy)"
  echo "  5. Copy the Client ID and Client Secret"
  echo ""
  echo "  TIP: Use --secret-name to read credentials from Secret Manager instead."
  echo ""
  read -rp "  Enter GOOGLE_CLIENT_ID: " CLIENT_ID
  read -rp "  Enter GOOGLE_CLIENT_SECRET: " CLIENT_SECRET

  if [[ -z "$CLIENT_ID" || -z "$CLIENT_SECRET" ]]; then
    echo "Error: Both CLIENT_ID and CLIENT_SECRET are required"
    exit 1
  fi
fi

# --- Step 3: Generate templ code and build with ko ---
REPO_ROOT=$(git rev-parse --show-toplevel)
IMAGE="us-central1-docker.pkg.dev/${PROJECT_ID}/cloud-run-source-deploy/runoauth"

echo ">>> Step 3: Generating templ code..."
cd "$REPO_ROOT"
go tool templ generate

echo ">>> Step 3b: Building and pushing image with ko..."
KO_DOCKER_REPO="$IMAGE" ko build ./cmd/runoauth --bare --platform=linux/amd64

# --- Step 4: Deploy the image with env vars ---
# KEY TEACHING MOMENT: With OAuth, the app handles authentication itself.
# We use --allow-unauthenticated because users need to reach the login page.
# This is the OPPOSITE of IAP, where --allow-unauthenticated is a security mistake.
echo ">>> Step 4: Deploying to Cloud Run (--allow-unauthenticated)..."
echo "  NOTE: Unlike IAP, OAuth apps MUST allow unauthenticated access"
echo "  because the app itself handles the login flow."

# Try to get an existing service URL; for new services, predict the URL format.
SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" --region="$REGION" --format='value(status.url)' 2>/dev/null || true)
if [[ -z "$SERVICE_URL" ]]; then
  PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')
  SERVICE_URL="https://${SERVICE_NAME}-${PROJECT_NUMBER}.${REGION}.run.app"
  echo "  New service — predicted URL: $SERVICE_URL"
fi
REDIRECT_URL="${SERVICE_URL}/auth/callback"

if [[ -n "$SECRET_NAME" ]]; then
  # Grant the Cloud Run service account access to the secret.
  SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
  if [[ -z "${PROJECT_NUMBER:-}" ]]; then
    PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')
    SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
  fi
  echo "  Granting secret access to ${SA}..."
  gcloud secrets add-iam-policy-binding "$SECRET_NAME" \
    --member="serviceAccount:${SA}" \
    --role="roles/secretmanager.secretAccessor" \
    --project="$PROJECT_ID" --quiet

  gcloud run deploy "$SERVICE_NAME" \
    --image="$IMAGE" \
    --region="$REGION" \
    --allow-unauthenticated \
    --port=8080 \
    --cpu=1 \
    --memory=256Mi \
    --min-instances=0 \
    --max-instances=3 \
    --concurrency=80 \
    --set-env-vars="GOOGLE_CLIENT_ID=${CLIENT_ID},OAUTH_REDIRECT_URL=${REDIRECT_URL}" \
    --set-secrets="GOOGLE_CLIENT_SECRET=${SECRET_NAME}:latest"
else
  gcloud run deploy "$SERVICE_NAME" \
    --image="$IMAGE" \
    --region="$REGION" \
    --allow-unauthenticated \
    --port=8080 \
    --cpu=1 \
    --memory=256Mi \
    --min-instances=0 \
    --max-instances=3 \
    --concurrency=80 \
    --set-env-vars="GOOGLE_CLIENT_ID=${CLIENT_ID},GOOGLE_CLIENT_SECRET=${CLIENT_SECRET},OAUTH_REDIRECT_URL=${REDIRECT_URL}"
fi

# --- Step 5: Verify service URL and update redirect if needed ---
ACTUAL_URL=$(gcloud run services describe "$SERVICE_NAME" --region="$REGION" --format='value(status.url)')
if [[ "$ACTUAL_URL" != "$SERVICE_URL" ]]; then
  REDIRECT_URL="${ACTUAL_URL}/auth/callback"
  echo ">>> Step 5: Updating redirect URL to match actual service URL..."
  gcloud run services update "$SERVICE_NAME" \
    --region="$REGION" \
    --update-env-vars="OAUTH_REDIRECT_URL=${REDIRECT_URL}"
  SERVICE_URL="$ACTUAL_URL"
fi

# --- Done ---
echo ""
echo "=== Deployment complete ==="
echo "  Service URL:  $SERVICE_URL"
echo "  Redirect URI: $REDIRECT_URL"
echo ""
echo "  IMPORTANT: Add this redirect URI to your OAuth credentials:"
echo "  ${REDIRECT_URL}"
echo ""
echo "  Go to: https://console.cloud.google.com/apis/credentials"
echo "  Edit your OAuth client → Add '${REDIRECT_URL}' as an authorized redirect URI"
