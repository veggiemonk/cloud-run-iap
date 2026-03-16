#!/usr/bin/env bash
# One-time setup for Cloud Run source deploys (gcloud run deploy --source).
# Enables required APIs and grants the default compute service account the
# permissions Cloud Build needs to read/write source archives in Cloud Storage.
#
# Run this once per project before the first --source deploy.
#
# Usage:
#   ./deploy/setup-cloud-build.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

echo ">>> Setting up Cloud Build source deploy for project $PROJECT_ID"
echo ">>>   Service account: $COMPUTE_SA"

# Enable required APIs
echo ">>> Enabling APIs..."
gcloud services enable \
  run.googleapis.com \
  cloudbuild.googleapis.com \
  artifactregistry.googleapis.com

# Grant storage access so Cloud Build can read/write source archives
echo ">>> Granting storage.objectViewer to compute service account..."
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${COMPUTE_SA}" \
  --role=roles/storage.objectViewer \
  --condition=None \
  --quiet > /dev/null

echo ">>> Done. You can now run: gcloud run deploy <service> --source ."
