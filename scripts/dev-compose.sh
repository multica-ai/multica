#!/usr/bin/env bash
# Shared Docker Compose wrapper for the local development PostgreSQL project.
#
# The dev PostgreSQL container is shared by every checkout, but its Compose
# project name is pinned instead of being derived from the checkout directory.
# Compose defaults the project name to the directory that holds the compose
# file (usually "multica"), so running the dev tooling from a checkout whose
# directory shares a name with a self-hosted install adopted that install's
# containers: `docker compose up -d postgres` recreated `multica-postgres-1`
# and stopped the running deployment. Pin the name so the dev project can only
# ever touch its own containers (SHAR-208).
#
# Set MULTICA_PG_COMPOSE_PROJECT to point at a different project name on
# purpose (for example to keep using a container created before this change).

dev_compose_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dev_compose_project="${MULTICA_PG_COMPOSE_PROJECT:-multica-dev}"

# Run a docker compose command against the dev project, anchored to this
# checkout's compose file so the caller's cwd cannot select another project.
dev_compose() {
  docker compose \
    -p "$dev_compose_project" \
    -f "$dev_compose_root/docker-compose.yml" \
    --project-directory "$dev_compose_root" \
    "$@"
}
