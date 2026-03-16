#!/usr/bin/env bash
# One-time setup for IAP on Cloud Run.
# Enables APIs, grants IAP service agent invoker access, restricts domain access.
#
# Usage:
#   ./deploy/setup-iap.sh <service-name> [--region <region>] [--domain <domain>]
#
# Examples:
#   ./deploy/setup-iap.sh runiap
#   ./deploy/setup-iap.sh runiap --domain example.com

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_common.sh"

DOMAIN="myowndomain.com"

parse_args "$@"
for arg in "${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}"; do
  case "$arg" in
    --domain) ;; # handled below
    *) ;;
  esac
done

# Re-parse for --domain flag
set -- "$@"
while [[ ${#EXTRA_ARGS[@]} -gt 0 ]]; do
  case "${EXTRA_ARGS[0]}" in
    --domain) DOMAIN="${EXTRA_ARGS[1]}"; EXTRA_ARGS=("${EXTRA_ARGS[@]:2}") ;;
    *)        EXTRA_ARGS=("${EXTRA_ARGS[@]:1}") ;;
  esac
done

echo "=== IAP Setup for $SERVICE_NAME ==="
echo "  Project:  $PROJECT_ID ($PROJECT_NUMBER)"
echo "  Region:   $REGION"
echo "  Domain:   $DOMAIN"
echo ""

# --- Enable required APIs ---
echo ">>> Enabling required APIs..."
gcloud services enable \
  iap.googleapis.com \
  run.googleapis.com \
  artifactregistry.googleapis.com

# --- Grant IAP service agent invoker access ---
echo ">>> Granting IAP service agent invoker access..."
gcloud run services add-iam-policy-binding "$SERVICE_NAME" \
  --region="$REGION" \
  --member="serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com" \
  --role=roles/run.invoker

# --- Restrict access to domain ---
echo ">>> Restricting access to @${DOMAIN}..."
gcloud iap web add-iam-policy-binding \
  --member="domain:${DOMAIN}" \
  --role=roles/iap.httpsResourceAccessor \
  --region="$REGION" \
  --resource-type=cloud-run \
  --service="$SERVICE_NAME"

echo ""
echo "=== IAP setup complete ==="
echo "  Service:  $SERVICE_NAME"
echo "  Domain:   @${DOMAIN}"
echo ""
echo "  Next: run ./deploy/deploy-iap.sh $SERVICE_NAME to build and deploy."
