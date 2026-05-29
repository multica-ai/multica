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
# With no image args, builds backend/web/postgres/controller. Pass `runtime`
# explicitly to build the runtime base + runtime-claude images (Plan C).

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
  [controller]="packaging/docker/controller/Dockerfile"
)

build_runtime() {
  local tag="$1" registry="$2" push="$3" platform="$4"
  local base="$registry/multica-runtime-base:$tag"
  local claude="$registry/multica-runtime-claude:$tag"

  # Embed a real version into the multica binary so the UI's CLI-version
  # gate (MIN_QUICK_CREATE_CLI_VERSION) accepts it. `git describe` produces
  # the dev-describe shape (vX.Y.Z-N-g<sha>) the gate exempts.
  local version="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
  local commit="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"

  echo "==> Building $base (version=$version commit=$commit)"
  docker build --platform "$platform" \
    --build-arg VERSION="$version" \
    --build-arg COMMIT="$commit" \
    -f packaging/docker/runtime/Dockerfile.base \
    -t "$base" .
  if [[ "$push" -eq 1 ]]; then
    echo "==> Pushing $base"
    docker push "$base"
  fi

  echo "==> Building $claude (FROM $base)"
  docker build --platform "$platform" \
    --build-arg BASE_IMAGE="$base" \
    -f packaging/docker/runtime/Dockerfile.claude \
    -t "$claude" .
  if [[ "$push" -eq 1 ]]; then
    echo "==> Pushing $claude"
    docker push "$claude"
  fi
}

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
  if [[ "$name" == "runtime" ]]; then
    build_runtime "$TAG" "$REGISTRY" "$PUSH" "$PLATFORM"
    continue
  fi
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
