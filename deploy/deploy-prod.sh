#!/usr/bin/env bash
# Build and deploy the production OAuth app to Cloud Run.
# Run setup-prod.sh first for one-time infrastructure setup.
#
# Usage:
#   ./deploy/deploy-prod.sh <service-name> [--region <region>] [--allowed-domain <domain>]
#
# Examples:
#   ./deploy/deploy-prod.sh runoauthprod
#   ./deploy/deploy-prod.sh runoauthprod --allowed-domain example.com

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_common.sh"

ALLOWED_DOMAIN="myowndomain.com"

parse_args "$@"
while [[ ${#EXTRA_ARGS[@]} -gt 0 ]]; do
  case "${EXTRA_ARGS[0]}" in
    --allowed-domain) ALLOWED_DOMAIN="${EXTRA_ARGS[1]}"; EXTRA_ARGS=("${EXTRA_ARGS[@]:2}") ;;
    *)                EXTRA_ARGS=("${EXTRA_ARGS[@]:1}") ;;
  esac
done

IMAGE="${KO_DOCKER_REPO_BASE}/runoauthprod"

echo "=== Deploying $SERVICE_NAME (production OAuth) to Cloud Run ==="
echo "  Project:        $PROJECT_ID"
echo "  Region:         $REGION"
echo "  Allowed domain: $ALLOWED_DOMAIN"
echo ""

# --- Build and push ---
build_and_push runoauthprod

# --- Predict service URL ---
SERVICE_URL=$(get_service_url "$SERVICE_NAME")
if [[ -z "$SERVICE_URL" ]]; then
  SERVICE_URL="https://${SERVICE_NAME}-${PROJECT_NUMBER}.${REGION}.run.app"
  echo "  New service — predicted URL: $SERVICE_URL"
fi
REDIRECT_URL="${SERVICE_URL}/auth/callback"

# --- Deploy ---
echo ">>> Deploying to Cloud Run..."
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
  --set-secrets="SESSION_ENCRYPTION_KEY=session-key:latest,CSRF_KEY=csrf-key:latest,GOOGLE_OAUTH_CONFIG=oauth-config:latest" \
  --set-env-vars="PROJECT_ID=${PROJECT_ID},ALLOWED_DOMAIN=${ALLOWED_DOMAIN},OAUTH_REDIRECT_URL=${REDIRECT_URL}"

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
