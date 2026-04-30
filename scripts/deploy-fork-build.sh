#!/usr/bin/env bash
# scripts/deploy-fork-build.sh
#
# Pre-flight + dry-run + plan output for the Multica Fork production deploy on
# team.cuongpho.com. Run by Cuong from his own SSH session.
#
# This script is INTENTIONALLY plan-only:
#   - It pulls main, builds the fork images, prints the effective compose
#     config, and prints the pending DB migrations as a SQL plan.
#   - It NEVER runs `docker compose up`, NEVER applies migrations, and NEVER
#     touches a running prod container.
#
# Cuong executes the actual deploy commands the script prints at the end.

set -euo pipefail

# ---------- Resolve repo root ----------
# Honour MULTICA_FORK_DIR if set (operator override), else resolve from the
# script's own location so the script works regardless of where the fork is
# checked out (canonical: /root/multica-fork on terminator-9999).
if [ -n "${MULTICA_FORK_DIR:-}" ]; then
  REPO_ROOT="$MULTICA_FORK_DIR"
else
  REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

if [ ! -d "$REPO_ROOT/.git" ] && [ ! -f "$REPO_ROOT/.git" ]; then
  echo "ERROR: $REPO_ROOT is not a git checkout." >&2
  exit 1
fi

cd "$REPO_ROOT"

# ---------- Operator-tunable knobs ----------
COMPOSE_BASE="${COMPOSE_BASE:-docker-compose.selfhost.yml}"
COMPOSE_BUILD_OVERRIDE="${COMPOSE_BUILD_OVERRIDE:-docker-compose.selfhost.build.yml}"
# Production-only port-binding override (binds postgres/backend/frontend to
# 127.0.0.1 so nginx terminates TLS and we don't collide with king-postgres
# on :5432). Kept off git on purpose (host-specific paths + bind addresses);
# the script auto-includes it when present so the dry-run config matches
# what Cuong actually runs in step 5.
COMPOSE_PROD_OVERRIDE="${COMPOSE_PROD_OVERRIDE:-docker-compose.production.yml}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-king-postgres}"
HEALTHCHECK_URL="${HEALTHCHECK_URL:-https://api-team.cuongpho.com/healthz}"
ENV_FILE="${ENV_FILE:-.env}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-server/migrations}"

COMPOSE_FILES=(-f "$COMPOSE_BASE" -f "$COMPOSE_BUILD_OVERRIDE")
if [ -f "$COMPOSE_PROD_OVERRIDE" ]; then
  COMPOSE_FILES+=(-f "$COMPOSE_PROD_OVERRIDE")
  COMPOSE_PROD_PRESENT=1
else
  COMPOSE_PROD_PRESENT=0
fi

hr() { printf '\n========================================================\n%s\n========================================================\n' "$1"; }

# ---------- Step 0: Pre-flight ----------
hr "[0/5] Pre-flight"
echo "    REPO_ROOT             = $REPO_ROOT"
echo "    COMPOSE_BASE          = $COMPOSE_BASE"
echo "    COMPOSE_BUILD_OVERRIDE= $COMPOSE_BUILD_OVERRIDE"
if [ "$COMPOSE_PROD_PRESENT" -eq 1 ]; then
  echo "    COMPOSE_PROD_OVERRIDE = $COMPOSE_PROD_OVERRIDE (present, will be included)"
else
  echo "    COMPOSE_PROD_OVERRIDE = $COMPOSE_PROD_OVERRIDE (not present, skipped)"
  echo "                           WARN: without this file, ports default to host:5432"
  echo "                                 which collides with king-postgres on prod."
fi
echo "    POSTGRES_CONTAINER    = $POSTGRES_CONTAINER"
echo "    HEALTHCHECK_URL       = $HEALTHCHECK_URL"
echo "    ENV_FILE              = $ENV_FILE"

command -v git >/dev/null || { echo "ERROR: git not found"; exit 1; }
command -v docker >/dev/null || { echo "ERROR: docker not found"; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "ERROR: 'docker compose' plugin not available"; exit 1; }

[ -f "$COMPOSE_BASE" ] || { echo "ERROR: missing $COMPOSE_BASE in $REPO_ROOT"; exit 1; }
[ -f "$COMPOSE_BUILD_OVERRIDE" ] || { echo "ERROR: missing $COMPOSE_BUILD_OVERRIDE in $REPO_ROOT"; exit 1; }
# COMPOSE_PROD_OVERRIDE is intentionally optional — git-ignored on purpose.
[ -d "$MIGRATIONS_DIR" ] || { echo "ERROR: missing $MIGRATIONS_DIR in $REPO_ROOT"; exit 1; }

if [ ! -f "$ENV_FILE" ]; then
  echo "WARN: $ENV_FILE not found in $REPO_ROOT — migration dry-run will likely fail."
  echo "      Copy .env.example to .env and fill in king-postgres credentials before deploy."
fi

if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER"; then
  echo "WARN: container '$POSTGRES_CONTAINER' is not running."
  echo "      Migration dry-run will be skipped."
  POSTGRES_RUNNING=0
else
  POSTGRES_RUNNING=1
fi

# ---------- Step 1: Pull latest main ----------
hr "[1/5] git pull --ff-only origin main"
CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
echo "    Current branch: $CURRENT_BRANCH"
if [ "$CURRENT_BRANCH" != "main" ]; then
  echo "WARN: not on 'main' — pulling main into the current branch is refused."
  echo "      Checkout main before deploying:  git checkout main && git pull --ff-only"
else
  git pull --ff-only
fi
echo ""
echo "    HEAD: $(git --no-pager log -1 --oneline)"

# ---------- Step 2: Build fork images ----------
hr "[2/5] docker compose build (fork images, local tags)"
docker compose "${COMPOSE_FILES[@]}" build

# ---------- Step 3: Print effective compose config ----------
hr "[3/5] docker compose config (effective merged config)"
docker compose "${COMPOSE_FILES[@]}" config

# ---------- Step 4: Migration dry-run against king-postgres ----------
hr "[4/5] DB migration dry-run against $POSTGRES_CONTAINER (READ-ONLY)"

if [ "$POSTGRES_RUNNING" -ne 1 ]; then
  echo "    Skipped: $POSTGRES_CONTAINER not running."
else
  # Source env to pick up POSTGRES_USER / POSTGRES_DB. Subshell + 'set -a' keeps
  # the rest of the script's strict-mode behaviour intact.
  if [ -f "$ENV_FILE" ]; then
    # shellcheck disable=SC1090
    set -a; . "$ENV_FILE"; set +a
  fi
  PG_USER="${POSTGRES_USER:-multica}"
  PG_DB="${POSTGRES_DB:-multica}"
  echo "    Connecting as $PG_USER to db $PG_DB inside $POSTGRES_CONTAINER..."

  # Read applied versions. If the table does not exist yet, treat all
  # migrations as pending.
  set +e
  APPLIED="$(docker exec -i "$POSTGRES_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -At \
    -c "SELECT version FROM schema_migrations ORDER BY version" 2>/tmp/dfb-psql.err)"
  PSQL_RC=$?
  set -e

  if [ $PSQL_RC -ne 0 ]; then
    if grep -qi "schema_migrations" /tmp/dfb-psql.err && grep -qi "does not exist" /tmp/dfb-psql.err; then
      echo "    schema_migrations table not found — treating all migrations as pending."
      APPLIED=""
    else
      echo "ERROR: psql failed against $POSTGRES_CONTAINER:"
      cat /tmp/dfb-psql.err >&2
      exit 1
    fi
  fi

  # Build the pending list = files in $MIGRATIONS_DIR/*.up.sql whose version
  # is not in $APPLIED. Version key matches server/internal/migrations.ExtractVersion:
  # the full filename minus the .up.sql / .down.sql suffix.
  PENDING=()
  for f in "$MIGRATIONS_DIR"/*.up.sql; do
    [ -e "$f" ] || continue
    version="$(basename "$f" .up.sql)"
    if ! printf '%s\n' "$APPLIED" | grep -qxF "$version"; then
      PENDING+=("$f")
    fi
  done

  if [ "${#PENDING[@]}" -eq 0 ]; then
    echo "    No pending migrations. Schema is up to date."
  else
    echo "    Pending migrations (${#PENDING[@]}):"
    for f in "${PENDING[@]}"; do
      echo "      - $f"
    done
    echo ""
    echo "    --- SQL plan (concatenated, in order) ---"
    for f in "${PENDING[@]}"; do
      echo ""
      echo "    -- BEGIN $f --"
      sed 's/^/    /' "$f"
      echo "    -- END $f --"
    done
    echo ""
    echo "    NOTE: this plan was NOT applied. The migration runs automatically"
    echo "          from the backend container's entrypoint when Cuong runs"
    echo "          'docker compose up -d' (see step 5)."
  fi
fi

# ---------- Step 5: Print Cuong's commands ----------
hr "[5/5] DRY RUN COMPLETE — Cuong runs the following from his own SSH session"

# Build the printed up/rollback commands so they match the COMPOSE_FILES the
# script actually used above (i.e. include docker-compose.production.yml when
# it is present on the host). Without this, the printed `up -d` would expose
# postgres on host :5432 and collide with king-postgres.
UP_FLAGS="-f $COMPOSE_BASE -f $COMPOSE_BUILD_OVERRIDE"
ROLLBACK_FLAGS="-f $COMPOSE_BASE"
if [ "$COMPOSE_PROD_PRESENT" -eq 1 ]; then
  UP_FLAGS="$UP_FLAGS -f $COMPOSE_PROD_OVERRIDE"
  ROLLBACK_FLAGS="$ROLLBACK_FLAGS -f $COMPOSE_PROD_OVERRIDE"
fi

cat <<EOF

Step A. Apply migrations + start the fork stack (single command — the backend
        entrypoint runs migrate-up before booting the server):

  cd $REPO_ROOT
  docker compose $UP_FLAGS up -d

Step B. Verify the deploy is healthy (give the backend ~30s to migrate + boot):

  docker ps --filter name=multica
  curl -sS $HEALTHCHECK_URL && echo

Rollback (revert to upstream GHCR images, no fork build):

  cd $REPO_ROOT
  docker compose $ROLLBACK_FLAGS up -d

Notes:
  - Migrations are applied by the backend entrypoint (./migrate up). The plan
    above is informational; it shows you what the entrypoint will execute.
  - If the healthcheck fails or migrations error out, follow the rollback
    above and post on the master issue. Do not hand-edit the schema.
  - This script never executes any of the commands above; you do.

EOF
