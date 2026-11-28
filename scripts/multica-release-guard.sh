#!/usr/bin/env bash

# Read-only production release preflight. This script never pulls, builds,
# starts, recreates, stops, migrates, or deletes containers, images, or volumes.
# Optional provenance assertions can be supplied without exposing credentials:
# EXPECTED_SOURCE_COMMIT=<sha> EXPECTED_OCI_REVISION=<sha> \
# EXPECTED_OCI_DIGEST='repo@sha256:...' scripts/multica-release-guard.sh

set -euo pipefail

SOURCE_DIR=${SOURCE_DIR:-/Users/mac/Documents/vibcoding/multica-source}
AUDIT_DIR=${AUDIT_DIR:-/opt/multica}
COMPOSE_FILE=${COMPOSE_FILE:-docker-compose.selfhost.yml}
EXPECTED_SOURCE_COMMIT=${EXPECTED_SOURCE_COMMIT:-}
EXPECTED_OCI_REVISION=${EXPECTED_OCI_REVISION:-}
EXPECTED_OCI_DIGEST=${EXPECTED_OCI_DIGEST:-}

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

note() {
  printf '%s\n' "$1"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is missing: $1"
}

require_command git
require_command docker
require_command awk
require_command grep

[[ -d "$SOURCE_DIR/.git" ]] || fail "development source is not a Git checkout: $SOURCE_DIR"
[[ -d "$AUDIT_DIR" ]] || fail "production audit directory is missing: $AUDIT_DIR"
[[ -f "$AUDIT_DIR/$COMPOSE_FILE" ]] || fail "Compose file is missing: $AUDIT_DIR/$COMPOSE_FILE"

source_branch=$(git -C "$SOURCE_DIR" branch --show-current)
source_commit=$(git -C "$SOURCE_DIR" rev-parse HEAD)
[[ -z "$(git -C "$SOURCE_DIR" status --short)" ]] || fail "development source checkout is dirty"

audit_commit=$(git -C "$AUDIT_DIR" rev-parse HEAD 2>/dev/null || true)
[[ -n "$audit_commit" ]] || fail "cannot read audit checkout commit from $AUDIT_DIR"
[[ -z "$EXPECTED_SOURCE_COMMIT" || "$source_commit" == "$EXPECTED_SOURCE_COMMIT" ]] \
  || fail "source commit does not match EXPECTED_SOURCE_COMMIT"
[[ -z "$EXPECTED_SOURCE_COMMIT" || "$audit_commit" == "$EXPECTED_SOURCE_COMMIT" ]] \
  || fail "audit checkout commit does not match EXPECTED_SOURCE_COMMIT"

note "source commit: $source_commit"
note "source branch: ${source_branch:-detached HEAD}"
note "audit commit: $audit_commit"

[[ -z "${COMPOSE_FILE:-}" || "$COMPOSE_FILE" == docker-compose.selfhost.yml ]] \
  || fail "production must use docker-compose.selfhost.yml only"
[[ -z "${COMPOSE_OVERRIDE_PATH:-}" ]] || fail "Compose overrides are prohibited"
[[ -z "${COMPOSE_PROFILES:-}" || "$COMPOSE_PROFILES" == collab ]] \
  || fail "unexpected Compose profiles: $COMPOSE_PROFILES"

compose=(docker compose -f "$AUDIT_DIR/$COMPOSE_FILE")
config=$(
  cd "$AUDIT_DIR"
  "${compose[@]}" config
)

printf '%s\n' "$config" | grep -Eq '^[[:space:]]+build:' && fail "Compose build override is active"
printf '%s\n' "$config" | grep -Eq 'docker-compose\.selfhost\.build\.yml|docker-compose\.override\.yml' \
  && fail "Compose override reference is active"

images=$(printf '%s\n' "$config" | awk '$1 == "image:" {print $2}')
[[ -n "$images" ]] || fail "Compose has no image services"
backend_image=$(printf '%s\n' "$images" | grep 'multica-backend:' | head -n 1)
web_image=$(printf '%s\n' "$images" | grep 'multica-web:' | head -n 1)
[[ -n "$backend_image" && -n "$web_image" ]] || fail "backend/web image pair is incomplete"

version_re='^ghcr\.io/multica-ai/multica-(backend|web|collab):v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$'
while IFS= read -r image; do
  [[ "$image" =~ $version_re ]] || fail "non-versioned or non-GHCR image: $image"
done <<< "$images"

backend_tag=${backend_image##*:}
web_tag=${web_image##*:}
[[ "$backend_tag" == "$web_tag" ]] || fail "backend/web image tags are not paired"
[[ "$backend_tag" != latest && "$backend_tag" != dev ]] || fail "latest/dev image tag is prohibited"
note "paired image tag: $backend_tag"

volumes=$(
  cd "$AUDIT_DIR"
  "${compose[@]}" config --volumes
)
printf '%s\n' "$volumes" | grep -qx 'pgdata' || fail "pgdata volume is not declared"
printf '%s\n' "$volumes" | grep -qx 'backend_uploads' || fail "backend_uploads volume is not declared"
note "protected volumes: pgdata, backend_uploads"

oci_revision=''
for service in backend frontend; do
  container_id=$(cd "$AUDIT_DIR" && "${compose[@]}" ps -q "$service")
  [[ -n "$container_id" ]] || fail "$service is not running"
  revision=$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$container_id")
  digest=$(docker inspect -f '{{join .RepoDigests "\n"}}' "$container_id" 2>/dev/null || true)
  [[ -n "$revision" && "$revision" != '<no value>' ]] || fail "$service OCI revision label is missing"
  [[ "$digest" == *'@sha256:'* ]] || fail "$service immutable OCI digest is missing"
  if [[ -z "$oci_revision" ]]; then
    oci_revision=$revision
  else
    [[ "$revision" == "$oci_revision" ]] || fail "backend/frontend OCI revisions are not paired"
  fi
  [[ -z "$EXPECTED_OCI_REVISION" || "$revision" == "$EXPECTED_OCI_REVISION" ]] \
    || fail "$service OCI revision does not match EXPECTED_OCI_REVISION"
  [[ -z "$EXPECTED_OCI_DIGEST" || "$digest" == "$EXPECTED_OCI_DIGEST" ]] \
    || fail "$service OCI digest does not match EXPECTED_OCI_DIGEST"
  note "$service OCI revision: $revision"
  note "$service OCI digest: $digest"
done

active_tasks=$(
  cd "$AUDIT_DIR"
  "${compose[@]}" exec -T postgres psql -Atqc \
    "SELECT count(*) FROM agent_task_queue WHERE status IN ('queued','dispatched','running','waiting_local_directory','deferred');"
)
[[ "$active_tasks" =~ ^[0-9]+$ ]] || fail "could not read active task count"
note "active tasks: $active_tasks"
[[ "$active_tasks" == 0 ]] || fail "active tasks block production recreation"

note "PASS: production release preflight is read-only and all checks passed"
