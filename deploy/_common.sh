#!/usr/bin/env bash
# Shared variables and functions for deploy scripts.
# Source this file — do not execute directly.
#
# Usage (from other deploy scripts):
#   SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#   source "$SCRIPT_DIR/_common.sh"

set -euo pipefail

PROJECT_ID=$(gcloud config get-value project 2>/dev/null)
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')
REGION="${REGION:-us-central1}"
REPO_ROOT=$(git rev-parse --show-toplevel)
KO_DOCKER_REPO_BASE="us-central1-docker.pkg.dev/${PROJECT_ID}/cloud-run-source-deploy"
COMPUTE_SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

# build_and_push <app-name>
# Generates templ code, builds with ko, and pushes to Artifact Registry.
# Returns the image reference via KO_DOCKER_REPO.
build_and_push() {
  local app="$1"
  cd "$REPO_ROOT"
  echo ">>> Generating templ code..."
  go tool templ generate
  echo ">>> Building and pushing image with ko..."
  KO_DOCKER_REPO="${KO_DOCKER_REPO_BASE}/${app}" ko build "./cmd/${app}" --bare --platform=linux/amd64
}

# get_service_url <service-name>
# Returns the Cloud Run service URL, or empty string if the service doesn't exist.
get_service_url() {
  gcloud run services describe "$1" --region="$REGION" --format='value(status.url)' 2>/dev/null || true
}

# parse_args <service-name> [--region <region>] [extra flags...]
# Sets SERVICE_NAME and REGION. Remaining args are left in EXTRA_ARGS.
parse_args() {
  if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <service-name> [--region <region>] [options...]"
    exit 1
  fi

  SERVICE_NAME="$1"
  shift

  EXTRA_ARGS=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --region) REGION="$2"; shift 2 ;;
      *)        EXTRA_ARGS+=("$1"); shift ;;
    esac
  done
}
