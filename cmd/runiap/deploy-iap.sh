#!/usr/bin/env bash
set -euo pipefail

# Deploy a Cloud Run service from source with IAP restricted to @myowndomain.com
#
# Usage:
#   ./deploy-iap.sh <service-name> [--region <region>] [--source <path>] [--port <port>]
#
# Examples:
#   ./deploy-iap.sh myapp
#   ./deploy-iap.sh myapp --region us-east1 --source ./app --port 3000

# --- Defaults ---
REGION="us-central1"
SOURCE="."
PORT="8080"

# --- Parse arguments ---
if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <service-name> [--region <region>] [--source <path>] [--port <port>]"
  exit 1
fi

SERVICE_NAME="$1"
shift

while [[ $# -gt 0 ]]; do
  case "$1" in
    --region) REGION="$2"; shift 2 ;;
    --source) SOURCE="$2"; shift 2 ;;
    --port)   PORT="$2";   shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

PROJECT_ID=$(gcloud config get-value project 2>/dev/null)
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')

echo "=== Deploying $SERVICE_NAME to Cloud Run with IAP ==="
echo "  Project:  $PROJECT_ID ($PROJECT_NUMBER)"
echo "  Region:   $REGION"
echo "  Source:   $SOURCE"
echo "  Port:     $PORT"
echo ""

# --- Step 1: Enable required APIs ---
echo ">>> Step 1: Enabling required APIs..."
gcloud services enable \
  iap.googleapis.com \
  run.googleapis.com \
  cloudbuild.googleapis.com \
  artifactregistry.googleapis.com

# --- Step 2: Grant storage access to compute service account ---
# Cloud Build needs this to read/write source archives
echo ">>> Step 2: Granting storage access to compute service account..."
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
  --role=roles/storage.objectViewer \
  --condition=None \
  --quiet > /dev/null

# --- Step 3: Deploy from source with IAP enabled ---
echo ">>> Step 3: Deploying from source (this may take a few minutes)..."
gcloud run deploy "$SERVICE_NAME" \
  --source "$SOURCE" \
  --region="$REGION" \
  --no-allow-unauthenticated \
  --iap \
  --port="$PORT" \
  --cpu=1 \
  --memory=256Mi \
  --min-instances=0 \
  --max-instances=3 \
  --concurrency=80

# --- Step 4: Grant IAP service agent invoker access ---
echo ">>> Step 4: Granting IAP service agent invoker access..."
gcloud run services add-iam-policy-binding "$SERVICE_NAME" \
  --region="$REGION" \
  --member="serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com" \
  --role=roles/run.invoker

# --- Step 5: Restrict access to @myowndomain.com ---
echo ">>> Step 5: Restricting access to @myowndomain.com..."
gcloud iap web add-iam-policy-binding \
  --member=domain:myowndomain.com \
  --role=roles/iap.httpsResourceAccessor \
  --region="$REGION" \
  --resource-type=cloud-run \
  --service="$SERVICE_NAME"

# --- Step 6: Set IAP_AUDIENCE env var ---
IAP_AUDIENCE="/projects/${PROJECT_NUMBER}/locations/${REGION}/services/${SERVICE_NAME}"
echo ">>> Step 6: Setting IAP_AUDIENCE=${IAP_AUDIENCE}..."
gcloud run services update "$SERVICE_NAME" \
  --region="$REGION" \
  --update-env-vars="IAP_AUDIENCE=${IAP_AUDIENCE}"

# --- Done ---
SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" --region="$REGION" --format='value(status.url)')
echo ""
echo "=== Deployment complete ==="
echo "  Service URL:  $SERVICE_URL"
echo "  IAP Audience: $IAP_AUDIENCE"
echo "  Access:       @myowndomain.com only"
