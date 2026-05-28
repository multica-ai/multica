#!/usr/bin/env bash
# packaging/scripts/build-images.sh
#
# Build and (optionally) push the images Multica needs for a self-hosted
# Kubernetes deployment. Plan A scope: backend, web, postgres.
# Plans C/D extend this script with runtime + controller + repo-cache.
#
# Usage:
#   ./build-images.sh [--no-push] [--registry REG] [--tag TAG] [image…]
#
# Defaults: registry=ghcr.io/chrissnell, tag=$(git rev-parse --short HEAD).
# With no image args, builds all known images.

set -euo pipefail

REGISTRY="${REGISTRY:-ghcr.io/chrissnell}"
TAG="${TAG:-$(git rev-parse --short HEAD)}"
# K8s nodes are amd64; default to that even when building on Apple Silicon.
PLATFORM="${PLATFORM:-linux/amd64}"
PUSH=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-push) PUSH=0; shift ;;
    --registry) REGISTRY="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --platform) PLATFORM="$2"; shift 2 ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) break ;;
  esac
done

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

declare -A IMAGES=(
  [backend]="Dockerfile"
  [web]="Dockerfile.web"
  [postgres]="packaging/docker/postgres/Dockerfile"
)

# If positional args given, restrict to those; else build all.
if [[ $# -gt 0 ]]; then
  SELECTED=("$@")
else
  SELECTED=("${!IMAGES[@]}")
fi

echo "==> Registry: $REGISTRY"
echo "==> Tag:      $TAG"
echo "==> Platform: $PLATFORM"
echo "==> Images:   ${SELECTED[*]}"
echo "==> Push:     $PUSH"
echo

for name in "${SELECTED[@]}"; do
  dockerfile="${IMAGES[$name]:-}"
  if [[ -z "$dockerfile" ]]; then
    echo "unknown image: $name" >&2
    exit 1
  fi
  full="$REGISTRY/multica-$name:$TAG"
  echo "==> Building $full from $dockerfile"
  docker build --platform "$PLATFORM" -f "$dockerfile" -t "$full" .
  if [[ "$PUSH" -eq 1 ]]; then
    echo "==> Pushing $full"
    docker push "$full"
  fi
done

echo
echo "==> Done. Tag used: $TAG"
