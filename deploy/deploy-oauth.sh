#!/usr/bin/env bash
# Build and deploy the OAuth demo app to Cloud Run.
# Run setup-oauth.sh first for one-time infrastructure setup.
#
# Usage:
#   ./deploy/deploy-oauth.sh <service-name> [--region <region>] [--secret-name <name>]
#
# Examples:
#   ./deploy/deploy-oauth.sh runoauth
#   ./deploy/deploy-oauth.sh runoauth --secret-name internal-tools-google-oauth-secret

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_common.sh"

SECRET_NAME=""

parse_args "$@"
while [[ ${#EXTRA_ARGS[@]} -gt 0 ]]; do
  case "${EXTRA_ARGS[0]}" in
    --secret-name) SECRET_NAME="${EXTRA_ARGS[1]}"; EXTRA_ARGS=("${EXTRA_ARGS[@]:2}") ;;
    *)             EXTRA_ARGS=("${EXTRA_ARGS[@]:1}") ;;
  esac
done

IMAGE="${KO_DOCKER_REPO_BASE}/runoauth"

echo "=== Deploying $SERVICE_NAME to Cloud Run with OAuth ==="
echo "  Project:  $PROJECT_ID"
echo "  Region:   $REGION"
echo ""

# --- Get OAuth credentials ---
if [[ -n "$SECRET_NAME" ]]; then
  echo ">>> Reading credentials from Secret Manager: $SECRET_NAME"
  SECRET_JSON=$(gcloud secrets versions access latest --secret="$SECRET_NAME" 2>/dev/null) || {
    echo "Error: Could not read secret '$SECRET_NAME' from Secret Manager"
    exit 1
  }
  CLIENT_ID=$(echo "$SECRET_JSON" | jq -r '.web.client_id')
  CLIENT_SECRET=$(echo "$SECRET_JSON" | jq -r '.web.client_secret')
  if [[ -z "$CLIENT_ID" || "$CLIENT_ID" == "null" || -z "$CLIENT_SECRET" || "$CLIENT_SECRET" == "null" ]]; then
    echo "Error: Could not parse client_id or client_secret from secret JSON"
    exit 1
  fi
  echo "  Client ID: ${CLIENT_ID:0:20}..."
else
  echo ">>> OAuth credentials (interactive)"
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

# --- Build and push ---
build_and_push runoauth

# --- Predict service URL ---
SERVICE_URL=$(get_service_url "$SERVICE_NAME")
if [[ -z "$SERVICE_URL" ]]; then
  SERVICE_URL="https://${SERVICE_NAME}-${PROJECT_NUMBER}.${REGION}.run.app"
  echo "  New service — predicted URL: $SERVICE_URL"
fi
REDIRECT_URL="${SERVICE_URL}/auth/callback"

# --- Deploy ---
echo ">>> Deploying to Cloud Run (--allow-unauthenticated)..."
echo "  NOTE: Unlike IAP, OAuth apps MUST allow unauthenticated access"
echo "  because the app itself handles the login flow."

if [[ -n "$SECRET_NAME" ]]; then
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

# --- Verify and update redirect URL if needed ---
ACTUAL_URL=$(get_service_url "$SERVICE_NAME")
if [[ -n "$ACTUAL_URL" && "$ACTUAL_URL" != "$SERVICE_URL" ]]; then
  REDIRECT_URL="${ACTUAL_URL}/auth/callback"
  echo ">>> Updating redirect URL to match actual service URL..."
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
