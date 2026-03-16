#!/usr/bin/env bash
set -euo pipefail

# Tag and push a release. Triggers GitHub Actions → goreleaser → GitHub Release + GHCR.
#
# Usage:
#   ./deploy/publish.sh <version>
#
# Examples:
#   ./deploy/publish.sh v1.0.0
#   ./deploy/publish.sh v1.2.3-rc1

# Verify gh CLI is authenticated as veggiemonk
GH_USER=$(gh api user --jq '.login')
if [[ "$GH_USER" != "veggiemonk" ]]; then
  echo "Error: gh authenticated as '$GH_USER', expected 'veggiemonk'"
  exit 1
fi

VERSION="${1:?Usage: $0 <version>}"

echo ">>> Tagging $VERSION..."
git tag -s "$VERSION" -m "Release $VERSION"

echo ">>> Pushing tag to origin..."
git push origin "$VERSION"

echo ""
echo "=== Tag $VERSION pushed ==="
echo "  GitHub Actions will build and publish to:"
echo "  - GitHub Releases (binaries for linux/darwin x amd64/arm64)"
echo "  - ghcr.io/veggiemonk/cloud-run-auth/{runiap,runoauth,runoauthprod}"
