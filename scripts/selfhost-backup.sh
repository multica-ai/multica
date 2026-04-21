#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR="${1:-${DEPLOY_DIR:-}}"
DEPLOY_APP_USER="${2:-${DEPLOY_APP_USER:-}}"
DEPLOY_SHA="${3:-${DEPLOY_SHA:-manual}}"

if [[ -z "${DEPLOY_DIR}" ]]; then
  echo "DEPLOY_DIR is required" >&2
  exit 1
fi

if [[ -z "${DEPLOY_APP_USER}" ]]; then
  echo "DEPLOY_APP_USER is required" >&2
  exit 1
fi

if [[ ! -d "${DEPLOY_DIR}" ]]; then
  echo "deploy directory does not exist: ${DEPLOY_DIR}" >&2
  exit 1
fi

if [[ ! -d "${DEPLOY_DIR}/.git" ]]; then
  echo "deploy target is not a git worktree: ${DEPLOY_DIR}" >&2
  exit 1
fi

short_sha="$(printf '%s' "${DEPLOY_SHA}" | cut -c1-7)"
timestamp="$(date +%Y%m%d-%H%M%S)"
backup_root="/home/${DEPLOY_APP_USER}/multica-release-backups/${timestamp}-${short_sha}"

sudo mkdir -p "${backup_root}"
sudo chown -R "${DEPLOY_APP_USER}:${DEPLOY_APP_USER}" "/home/${DEPLOY_APP_USER}/multica-release-backups"

if [[ -f "${DEPLOY_DIR}/.env" ]]; then
  sudo cp "${DEPLOY_DIR}/.env" "${backup_root}/.env.backup"
fi

sudo -u "${DEPLOY_APP_USER}" git -C "${DEPLOY_DIR}" status --short > "${backup_root}/predeploy-status.txt"
sudo -u "${DEPLOY_APP_USER}" git -C "${DEPLOY_DIR}" diff --binary > "${backup_root}/predeploy-working-tree.patch"
sudo -u "${DEPLOY_APP_USER}" git -C "${DEPLOY_DIR}" diff --stat > "${backup_root}/predeploy-diffstat.txt"

sudo tar -czf "${backup_root}/worktree-no-git.tgz" \
  --exclude='.git' \
  -C "$(dirname "${DEPLOY_DIR}")" \
  "$(basename "${DEPLOY_DIR}")"

postgres_user="$(sudo docker exec multica-postgres-1 printenv POSTGRES_USER)"
postgres_password="$(sudo docker exec multica-postgres-1 printenv POSTGRES_PASSWORD)"
postgres_db="$(sudo docker exec multica-postgres-1 printenv POSTGRES_DB)"

sudo docker exec multica-postgres-1 sh -lc \
  "PGPASSWORD=\"${postgres_password}\" pg_dump -U \"${postgres_user}\" -d \"${postgres_db}\" -Fc" \
  > "${backup_root}/postgres.dump"

sudo docker exec multica-postgres-1 sh -lc \
  "PGPASSWORD=\"${postgres_password}\" pg_dumpall -U \"${postgres_user}\" --globals-only" \
  > "${backup_root}/postgres-globals.sql"

sudo docker run --rm \
  -v multica_backend_uploads:/from \
  -v "${backup_root}":/to \
  alpine sh -lc 'tar -czf /to/backend_uploads.tgz -C /from .'

sudo sha256sum \
  "${backup_root}/postgres.dump" \
  "${backup_root}/postgres-globals.sql" \
  "${backup_root}/backend_uploads.tgz" \
  "${backup_root}/worktree-no-git.tgz" \
  > "${backup_root}/SHA256SUMS"

if [[ -f "${backup_root}/.env.backup" ]]; then
  sudo sha256sum "${backup_root}/.env.backup" >> "${backup_root}/SHA256SUMS"
fi

sudo chown -R "${DEPLOY_APP_USER}:${DEPLOY_APP_USER}" "${backup_root}"

printf '%s\n' "${backup_root}"
