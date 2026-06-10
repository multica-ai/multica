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
# Defaults: registry=registry.chrissnell.com/multica, tag=$(git rev-parse --short HEAD).
# Override with --registry ghcr.io/chrissnell to use the old public registry.
# With no image args, builds backend/web/postgres/controller. Pass `runtime`
# explicitly to build the runtime base + runtime-claude images (Plan C).

set -euo pipefail

REGISTRY="${REGISTRY:-registry.chrissnell.com/multica}"
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

# Read a version pin from packaging/<name>. Strips whitespace and fails if
# the file is missing or empty — an unpinned build should fail loudly, not
# silently resolve to "latest".
read_pin() {
  local name="$1"
  local file="$ROOT/packaging/$name"
  [ -f "$file" ] || { echo "missing pin file: packaging/$name" >&2; exit 1; }
  local value
  value="$(tr -d '[:space:]' < "$file")"
  [ -n "$value" ] || { echo "packaging/$name is empty" >&2; exit 1; }
  printf '%s' "$value"
}

declare -A IMAGES=(
  [backend]="Dockerfile"
  [web]="Dockerfile.web"
  [postgres]="packaging/docker/postgres/Dockerfile"
  [controller]="packaging/docker/controller/Dockerfile"
  [claude-broker]="packaging/docker/claude-broker/Dockerfile"
  [repocache]="packaging/docker/repocache/Dockerfile"
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

  # Pin the claude-code version from packaging/claude-code-version. The
  # watcher workflow (.github/workflows/claude-version-watch.yml) bumps
  # this file via PR when a new release lands on npm.
  local claude_code_version
  claude_code_version="$(read_pin claude-code-version)"

  # Toolchain pins for the runtime base. Each lives in its own text file
  # so future watcher workflows can bump them via PR (same pattern as
  # claude-code-version).
  local rust_version kotlin_version golangci_lint_version ktlint_version pnpm_version kubectl_version helm_version gh_version
  rust_version="$(read_pin rust-version)"
  kotlin_version="$(read_pin kotlin-version)"
  golangci_lint_version="$(read_pin golangci-lint-version)"
  ktlint_version="$(read_pin ktlint-version)"
  pnpm_version="$(read_pin pnpm-version)"
  kubectl_version="$(read_pin kubectl-version)"
  helm_version="$(read_pin helm-version)"
  gh_version="$(read_pin gh-version)"

  echo "==> Building $base (version=$version commit=$commit)"
  echo "    rust=$rust_version kotlin=$kotlin_version pnpm=$pnpm_version"
  echo "    golangci-lint=$golangci_lint_version ktlint=$ktlint_version"
  echo "    kubectl=$kubectl_version helm=$helm_version gh=$gh_version"
  docker build --platform "$platform" \
    --build-arg VERSION="$version" \
    --build-arg COMMIT="$commit" \
    --build-arg RUST_VERSION="$rust_version" \
    --build-arg KOTLIN_VERSION="$kotlin_version" \
    --build-arg GOLANGCI_LINT_VERSION="$golangci_lint_version" \
    --build-arg KTLINT_VERSION="$ktlint_version" \
    --build-arg PNPM_VERSION="$pnpm_version" \
    --build-arg KUBECTL_VERSION="$kubectl_version" \
    --build-arg HELM_VERSION="$helm_version" \
    --build-arg GH_VERSION="$gh_version" \
    -f packaging/docker/runtime/Dockerfile.base \
    -t "$base" .
  if [[ "$push" -eq 1 ]]; then
    echo "==> Pushing $base"
    docker push "$base"
  fi

  echo "==> Building $claude (FROM $base, claude-code=$claude_code_version)"
  docker build --platform "$platform" \
    --build-arg BASE_IMAGE="$base" \
    --build-arg CLAUDE_CODE_VERSION="$claude_code_version" \
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
