#!/usr/bin/env bash
# Build and deploy the IAP app to Cloud Run.
# Run setup-iap.sh first for one-time infrastructure setup.
#
# Usage:
#   ./deploy/deploy-iap.sh <service-name> [--region <region>] [--port <port>]
#
# Examples:
#   ./deploy/deploy-iap.sh runiap
#   ./deploy/deploy-iap.sh runiap --region us-east1 --port 3000

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_common.sh"

PORT="8080"

parse_args "$@"
# Re-parse extra args for --port
while [[ ${#EXTRA_ARGS[@]} -gt 0 ]]; do
  case "${EXTRA_ARGS[0]}" in
    --port) PORT="${EXTRA_ARGS[1]}"; EXTRA_ARGS=("${EXTRA_ARGS[@]:2}") ;;
    *)      EXTRA_ARGS=("${EXTRA_ARGS[@]:1}") ;;
  esac
done

IMAGE="${KO_DOCKER_REPO_BASE}/runiap"

echo "=== Deploying $SERVICE_NAME to Cloud Run with IAP ==="
echo "  Project:  $PROJECT_ID ($PROJECT_NUMBER)"
echo "  Region:   $REGION"
echo "  Port:     $PORT"
echo ""

# --- Build and push ---
build_and_push runiap

# --- Deploy ---
echo ">>> Deploying image to Cloud Run with IAP..."
gcloud run deploy "$SERVICE_NAME" \
  --image="$IMAGE" \
  --region="$REGION" \
  --no-allow-unauthenticated \
  --iap \
  --port="$PORT" \
  --cpu=1 \
  --memory=256Mi \
  --min-instances=0 \
  --max-instances=3 \
  --concurrency=80

# --- Set IAP_AUDIENCE ---
IAP_AUDIENCE="/projects/${PROJECT_NUMBER}/locations/${REGION}/services/${SERVICE_NAME}"
echo ">>> Setting IAP_AUDIENCE=${IAP_AUDIENCE}..."
gcloud run services update "$SERVICE_NAME" \
  --region="$REGION" \
  --update-env-vars="IAP_AUDIENCE=${IAP_AUDIENCE}"

# --- Done ---
SERVICE_URL=$(get_service_url "$SERVICE_NAME")
echo ""
echo "=== Deployment complete ==="
echo "  Service URL:  $SERVICE_URL"
echo "  IAP Audience: $IAP_AUDIENCE"
