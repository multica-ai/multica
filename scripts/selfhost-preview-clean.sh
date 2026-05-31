#!/usr/bin/env bash
set -euo pipefail

# Stop a self-host preview runtime by Issue/profile. This intentionally does
# not delete Postgres containers, volumes, or databases; local test data is
# long-lived and cleanup is limited to the Docker Compose runtime.

slugify() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//'
}

issue="${ISSUE:-}"
if [ -n "$issue" ]; then
  issue="$(printf '%s' "$issue" | tr '[:lower:]' '[:upper:]')"
fi

if [ -n "${PROFILE:-}" ]; then
  profile="$(slugify "$PROFILE")"
elif [ -n "$issue" ]; then
  profile="$(slugify "$issue")"
else
  profile="$(slugify "${WORKTREE_NAME:-$(basename "$PWD")}")"
fi
if [ -z "$profile" ]; then
  echo "Cannot derive preview profile. Pass ISSUE=OPE-xxxx or PROFILE=name."
  exit 1
fi

project_name="${COMPOSE_PROJECT_NAME:-multica_preview_${profile}}"

echo "==> Stopping self-host preview runtime..."
if [ -n "$issue" ]; then
  echo "Issue:           $issue"
fi
echo "Preview profile: $profile"
echo "Compose project: $project_name"

docker compose \
  -p "$project_name" \
  -f docker-compose.selfhost.yml \
  -f docker-compose.selfhost.build.yml \
  down

echo "✓ Preview runtime stopped. Database/Postgres were left untouched."
