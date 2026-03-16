#!/usr/bin/env bash
# One-time setup for the production OAuth app on Cloud Run.
# Enables APIs, creates Firestore database + TTL, creates secrets, grants IAM.
#
# Usage:
#   ./deploy/setup-prod.sh <service-name> [--region <region>] [--oauth-secret <path-or-secret-name>]
#
# Examples:
#   ./deploy/setup-prod.sh runoauthprod
#   ./deploy/setup-prod.sh runoauthprod --oauth-secret client_secret_12345.json
#   ./deploy/setup-prod.sh runoauthprod --oauth-secret my-existing-secret

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_common.sh"

OAUTH_SECRET_INPUT=""

parse_args "$@"
while [[ ${#EXTRA_ARGS[@]} -gt 0 ]]; do
  case "${EXTRA_ARGS[0]}" in
    --oauth-secret) OAUTH_SECRET_INPUT="${EXTRA_ARGS[1]}"; EXTRA_ARGS=("${EXTRA_ARGS[@]:2}") ;;
    *)              EXTRA_ARGS=("${EXTRA_ARGS[@]:1}") ;;
  esac
done

echo "=== Production OAuth Setup for $SERVICE_NAME ==="
echo "  Project:  $PROJECT_ID ($PROJECT_NUMBER)"
echo "  Region:   $REGION"
echo ""

# --- Enable required APIs ---
echo ">>> Enabling required APIs..."
gcloud services enable \
  run.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  firestore.googleapis.com

# --- Create Firestore database ---
echo ">>> Setting up Firestore..."
if gcloud firestore databases describe --database="(default)" --project="$PROJECT_ID" &>/dev/null; then
  echo "  Firestore (default) database already exists."
else
  echo "  Creating Firestore (default) database..."
  gcloud firestore databases create \
    --database="(default)" \
    --location="$REGION" \
    --project="$PROJECT_ID"
fi

# --- Create TTL policy for session cleanup ---
echo ">>> Setting up TTL policy on sessions collection..."
gcloud firestore fields ttls update ExpiresAt \
  --collection-group=sessions \
  --enable-ttl \
  --project="$PROJECT_ID" 2>/dev/null || echo "  TTL policy already configured or pending."

# --- Create secrets ---
create_secret_if_missing() {
  local name="$1"
  if gcloud secrets describe "$name" --project="$PROJECT_ID" &>/dev/null; then
    echo "  Secret '$name' already exists — skipping."
    return 0
  fi
  return 1
}

# oauth-config secret
echo ""
echo ">>> Setting up oauth-config secret..."
if ! create_secret_if_missing "oauth-config" "OAuth client JSON"; then
  if [[ -n "$OAUTH_SECRET_INPUT" ]]; then
    if [[ -f "$OAUTH_SECRET_INPUT" ]]; then
      echo "  Creating oauth-config from file: $OAUTH_SECRET_INPUT"
      gcloud secrets create oauth-config \
        --data-file="$OAUTH_SECRET_INPUT" \
        --project="$PROJECT_ID"
    else
      # Assume it's an existing secret name — copy its latest version
      echo "  Creating oauth-config from existing secret: $OAUTH_SECRET_INPUT"
      gcloud secrets versions access latest --secret="$OAUTH_SECRET_INPUT" --project="$PROJECT_ID" | \
        gcloud secrets create oauth-config --data-file=- --project="$PROJECT_ID"
    fi
  else
    echo ""
    echo "  You need to provide your OAuth client JSON."
    echo "  Download it from: https://console.cloud.google.com/apis/credentials"
    echo ""
    read -rp "  Enter path to OAuth client JSON file: " OAUTH_FILE
    if [[ ! -f "$OAUTH_FILE" ]]; then
      echo "Error: File not found: $OAUTH_FILE"
      exit 1
    fi
    gcloud secrets create oauth-config \
      --data-file="$OAUTH_FILE" \
      --project="$PROJECT_ID"
  fi
fi

# session-key secret
echo ""
echo ">>> Setting up session-key secret..."
if ! create_secret_if_missing "session-key" "Session encryption key"; then
  echo "  Generating session encryption key (32 bytes, base64)..."
  openssl rand -base64 32 | \
    gcloud secrets create session-key --data-file=- --project="$PROJECT_ID"
fi

# csrf-key secret
echo ""
echo ">>> Setting up csrf-key secret..."
if ! create_secret_if_missing "csrf-key" "CSRF HMAC key"; then
  echo "  Generating CSRF key (32 bytes, base64)..."
  openssl rand -base64 32 | \
    gcloud secrets create csrf-key --data-file=- --project="$PROJECT_ID"
fi

# --- Grant compute SA access to secrets ---
echo ""
echo ">>> Granting secret access to ${COMPUTE_SA}..."
for secret in oauth-config session-key csrf-key; do
  gcloud secrets add-iam-policy-binding "$secret" \
    --member="serviceAccount:${COMPUTE_SA}" \
    --role="roles/secretmanager.secretAccessor" \
    --project="$PROJECT_ID" --quiet
  echo "  ✓ $secret"
done

# --- Grant Firestore access ---
echo ""
echo ">>> Granting Firestore access to ${COMPUTE_SA}..."
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${COMPUTE_SA}" \
  --role="roles/datastore.user" \
  --condition=None \
  --quiet > /dev/null
echo "  ✓ roles/datastore.user"

echo ""
echo "=== Production setup complete ==="
echo "  Secrets created: oauth-config, session-key, csrf-key"
echo "  Firestore: (default) database with TTL on sessions.ExpiresAt"
echo ""
echo "  Next: run ./deploy/deploy-prod.sh $SERVICE_NAME to build and deploy."
