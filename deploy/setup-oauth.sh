#!/usr/bin/env bash
# One-time setup for OAuth on Cloud Run.
# Enables APIs, reads OAuth credentials from Secret Manager, grants access.
#
# Usage:
#   ./deploy/setup-oauth.sh <service-name> [--region <region>] [--secret-name <name>]
#
# Examples:
#   ./deploy/setup-oauth.sh runoauth --secret-name internal-tools-google-oauth-secret

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

echo "=== OAuth Setup for $SERVICE_NAME ==="
echo "  Project:  $PROJECT_ID"
echo "  Region:   $REGION"
echo ""

# --- Enable required APIs ---
echo ">>> Enabling required APIs..."
gcloud services enable \
  run.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com

# --- OAuth credentials ---
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
    echo "  Expected format: {\"web\": {\"client_id\": \"...\", \"client_secret\": \"...\"}}"
    exit 1
  fi
  echo "  Client ID: ${CLIENT_ID:0:20}..."

  # Grant compute SA access to the secret
  echo ">>> Granting secret access to ${COMPUTE_SA}..."
  gcloud secrets add-iam-policy-binding "$SECRET_NAME" \
    --member="serviceAccount:${COMPUTE_SA}" \
    --role="roles/secretmanager.secretAccessor" \
    --project="$PROJECT_ID" --quiet
else
  echo ""
  echo "  No --secret-name provided."
  echo "  You must create OAuth 2.0 credentials in the Google Cloud Console:"
  echo "  1. Go to: https://console.cloud.google.com/apis/credentials"
  echo "  2. Click 'Create Credentials' → 'OAuth client ID'"
  echo "  3. Application type: 'Web application'"
  echo "  4. Add authorized redirect URI (will be shown after deploy)"
  echo ""
  echo "  TIP: Use --secret-name to read credentials from Secret Manager."
fi

echo ""
echo "=== OAuth setup complete ==="
echo "  Next: run ./deploy/deploy-oauth.sh $SERVICE_NAME to build and deploy."
