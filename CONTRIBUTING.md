# Contributing Guide

This guide documents the local development workflow for contributors working on the Multica codebase.

It covers:

- first-time setup
- day-to-day development in the main checkout
- isolated worktree development
- the shared PostgreSQL model
- testing and verification
- full-stack isolated testing (backend + frontend + daemon from source)
- troubleshooting and destructive reset options

## Development Model

Local development uses one shared PostgreSQL container and one database per checkout.

- the main checkout usually uses `.env` and `POSTGRES_DB=multica`
- each Git worktree uses its own `.env.worktree`
- every checkout connects to the same PostgreSQL host: `localhost:5432`
- isolation happens at the database level, not by starting a separate Docker Compose project
- backend and frontend ports are still unique per worktree

This keeps Docker simple while still isolating schema and data.

## Prerequisites

- Node.js `v20+`
- `pnpm` `v10.28+`
- Go `v1.26+`
- Docker

## Important Rules

- The main checkout should use `.env`.
- A worktree should use `.env.worktree`.
- Do not copy `.env` into a worktree directory.

Why:

- the current command flow prefers `.env` over `.env.worktree`
- if a worktree contains `.env`, it can accidentally point back to the main database

## Environment Files

### Main Checkout

Create `.env` once:

```bash
cp .env.example .env
```

By default, `.env` points to:

```bash
POSTGRES_DB=multica
POSTGRES_PORT=5432
DATABASE_URL=postgres://multica:multica@localhost:5432/multica?sslmode=disable
PORT=8080
FRONTEND_PORT=3000
```

### Worktree

Generate `.env.worktree` from inside the worktree:

```bash
make worktree-env
```

That generates values like:

```bash
POSTGRES_DB=multica_my_feature_702
POSTGRES_PORT=5432
PORT=18782
FRONTEND_PORT=13702
DATABASE_URL=postgres://multica:multica@localhost:5432/multica_my_feature_702?sslmode=disable
```

Notes:

- `POSTGRES_DB` is unique per worktree
- `POSTGRES_PORT` stays fixed at `5432`
- backend and frontend ports are derived from the worktree path hash
- `make worktree-env` refuses to overwrite an existing `.env.worktree`

To regenerate a worktree env file:

```bash
FORCE=1 make worktree-env
```

## First-Time Setup

### Quick Start (recommended)

From any checkout (main or worktree):

```bash
make dev
```

This single command:

- auto-detects whether you're in a main checkout or a worktree
- creates the appropriate env file (`.env` or `.env.worktree`) if it doesn't exist
- checks that prerequisites (Node.js, pnpm, Go, Docker) are installed
- installs JavaScript dependencies
- ensures the shared PostgreSQL container is running
- creates the application database if it does not exist
- runs all migrations
- starts both backend and frontend

### Explicit Setup (advanced)

If you prefer separate control over setup and startup:

#### Main Checkout

```bash
cp .env.example .env
make setup-main
make start-main
```

Stop:

```bash
make stop-main
```

#### Worktree

```bash
make worktree-env
make setup-worktree
make start-worktree
```

Stop:

```bash
make stop-worktree
```

## Recommended Daily Workflow

### Main Checkout

Use the main checkout when you want a stable local environment for `main`.

```bash
make start-main
make stop-main
make check-main
```

### Feature Worktree

Use a worktree when you want isolated data and separate app ports.

```bash
git worktree add ../multica-feature -b feat/my-change main
cd ../multica-feature
make dev
```

After that, day-to-day commands are:

```bash
make dev              # start (re-runs setup if needed, idempotent)
make stop-worktree    # stop
make check-worktree   # verify
```

## Running Main and Worktree at the Same Time

This is a first-class workflow.

Example:

- main checkout
  - database: `multica`
  - backend: `8080`
  - frontend: `3000`
- worktree checkout
  - database: `multica_my_feature_702`
  - backend: generated worktree port such as `18782`
  - frontend: generated worktree port such as `13702`

Both checkouts use:

- the same PostgreSQL container
- the same PostgreSQL port: `5432`

But they do not share application data, because each uses a different database.

## Command Reference

### Shared Infrastructure

Start the shared PostgreSQL container:

```bash
make db-up
```

Stop the shared PostgreSQL container:

```bash
make db-down
```

Important:

- `make db-down` stops the container but keeps the Docker volume
- your local databases are preserved

### App Lifecycle

Main checkout:

```bash
make setup-main
make start-main
make stop-main
make check-main
```

Worktree:

```bash
make worktree-env
make setup-worktree
make start-worktree
make stop-worktree
make check-worktree
```

Generic targets for the current checkout:

```bash
make setup
make start
make stop
make check
make dev
make test
make migrate-up
make migrate-down
```

These generic targets require a valid env file in the current directory.

## How Database Creation Works

Database creation is automatic.

The following commands all ensure the target database exists before they continue:

- `make setup`
- `make start`
- `make dev`
- `make test`
- `make migrate-up`
- `make migrate-down`
- `make check`

That logic lives in `scripts/ensure-postgres.sh`.

## Testing

Run all local checks:

```bash
make check-main
```

Or from a worktree:

```bash
make check-worktree
```

This runs:

1. TypeScript typecheck
2. TypeScript unit tests
3. Go tests
4. Playwright E2E tests

Notes:

- Go tests create their own fixture data
- E2E tests create their own workspace and issue fixtures
- the check flow starts backend/frontend only if they are not already running

## Local Codex Daemon

Run the local daemon:

```bash
make daemon
```

The daemon authenticates using the CLI's stored token (`multica login`).
It registers runtimes for all watched workspaces from the CLI config.

## Full-Stack Isolated Testing

This section covers running the complete stack (backend, frontend, daemon) from
source in a fully isolated environment. Useful for testing end-to-end changes
that span multiple components, or for automated CI/AI workflows that need zero
human intervention.

### Why Not Just `make daemon`?

`make daemon` uses the system-installed CLI's stored token and connects to
whatever server is configured in `~/.multica/config.json`. That's fine for
day-to-day development against a shared server, but for fully isolated testing
you need:

- a local backend and frontend (from source)
- a local daemon (from source) with its own profile
- automated authentication (no browser login)
- no interference with your production CLI config

### Dynamic Profile Naming

Each worktree must use a unique daemon profile to avoid collisions when
multiple features run in parallel.

The profile name is derived from the worktree directory using the same
slug + hash pattern as `scripts/init-worktree-env.sh`:

```bash
WORKTREE_DIR="$(basename "$PWD")"
SLUG="$(printf '%s' "$WORKTREE_DIR" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
HASH="$(printf '%s' "$PWD" | cksum | awk '{print $1}')"
OFFSET=$((HASH % 1000))
PROFILE="dev-${SLUG}-${OFFSET}"
```

Example: worktree at `../multica-feat-auth` produces profile
`dev-multica_feat_auth-347`, matching that worktree's port and database
allocation.

### Start the Isolated Environment

Run all steps from the worktree root (where the Makefile is).

#### 1. Start backend, frontend, and database

```bash
make dev
```

Wait for the backend to be healthy:

```bash
PORT=$(grep '^PORT=' .env.worktree 2>/dev/null || grep '^PORT=' .env | head -1 | cut -d= -f2)
PORT=${PORT:-8080}
SERVER="http://localhost:${PORT}"

for i in $(seq 1 30); do
  curl -sf "$SERVER/health" > /dev/null 2>&1 && break
  sleep 2
done
```

#### 2. Create a test user and token (automated auth)

For deterministic local automation, set `MULTICA_DEV_VERIFICATION_CODE=888888`
in your env file before starting the backend:

```bash
curl -s -X POST "$SERVER/auth/send-code" \
  -H "Content-Type: application/json" \
  -d '{"email": "dev@localhost"}'

JWT=$(curl -s -X POST "$SERVER/auth/verify-code" \
  -H "Content-Type: application/json" \
  -d '{"email": "dev@localhost", "code": "888888"}' | jq -r '.token')

PAT=$(curl -s -X POST "$SERVER/api/tokens" \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"name": "auto-dev", "expires_in_days": 365}' | jq -r '.token')
```

#### 3. Create a workspace

```bash
WS=$(curl -s -X POST "$SERVER/api/workspaces" \
  -H "Authorization: Bearer $PAT" \
  -H "Content-Type: application/json" \
  -d '{"name": "Dev", "slug": "dev"}' | jq -r '.id')
```

#### 4. Compute profile name and write CLI config

```bash
# Compute profile (see Dynamic Profile Naming above)
WORKTREE_DIR="$(basename "$PWD")"
SLUG="$(printf '%s' "$WORKTREE_DIR" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
HASH="$(printf '%s' "$PWD" | cksum | awk '{print $1}')"
OFFSET=$((HASH % 1000))
PROFILE="dev-${SLUG}-${OFFSET}"

FRONTEND_PORT=$(grep '^FRONTEND_PORT=' .env.worktree 2>/dev/null || grep '^FRONTEND_PORT=' .env | head -1 | cut -d= -f2)
FRONTEND_PORT=${FRONTEND_PORT:-3000}

CONFIG_DIR="$HOME/.multica/profiles/$PROFILE"
mkdir -p "$CONFIG_DIR"

cat > "$CONFIG_DIR/config.json" << EOF
{
  "server_url": "$SERVER",
  "app_url": "http://localhost:${FRONTEND_PORT}",
  "token": "$PAT",
  "workspace_id": "$WS",
  "watched_workspaces": [{"id": "$WS", "name": "Dev"}]
}
EOF
```

#### 5. Start the daemon from source

```bash
make cli ARGS="daemon start --profile $PROFILE"
```

The daemon runs from the current worktree's Go source, connecting to the
local backend. Agent-executed `multica` commands automatically use the same
binary (the daemon prepends its own directory to `PATH`).

### Stop the Isolated Environment

```bash
# Compute profile (same formula)
PROFILE="dev-$(printf '%s' "$(basename "$PWD")" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')-$(( $(printf '%s' "$PWD" | cksum | awk '{print $1}') % 1000 ))"

# 1. Stop daemon
make cli ARGS="daemon stop --profile $PROFILE"

# 2. Stop backend + frontend
make stop            # main checkout
make stop-worktree   # worktree checkout

# 3. (Optional) Stop shared PostgreSQL
make db-down

# 4. (Optional) Clean build artifacts
make clean

# 5. (Optional) Remove profile config
rm -rf "$HOME/.multica/profiles/$PROFILE"
```

### Desktop App Local Testing

To test the Electron desktop app against a local backend:

```bash
# After backend is running (make dev)
pnpm dev:desktop
```

This automatically:

1. Compiles the `multica` CLI from `server/cmd/multica` into
   `apps/desktop/resources/bin/multica`
2. Creates an isolated profile named `desktop-localhost-<PORT>`
3. Starts and manages its own daemon instance
4. Connects to the local backend

Login in the Desktop UI with `dev@localhost` and the generated code from the
backend logs. If you set `MULTICA_DEV_VERIFICATION_CODE=888888` before starting
the backend, you can use `888888` instead.

If the backend runs on a non-default port (worktree), create
`apps/desktop/.env.development.local`:

```bash
VITE_API_URL=http://localhost:<backend-port>
VITE_WS_URL=ws://localhost:<backend-port>/ws
```

### Isolation Guarantee

Nothing in this flow touches the system-installed `multica` or the default
`~/.multica/config.json`:

| Resource | System / Production | Local Dev (per-worktree) |
|---|---|---|
| Config | `~/.multica/config.json` | `~/.multica/profiles/dev-<slug>-<hash>/config.json` |
| Daemon PID | `~/.multica/daemon.pid` | `~/.multica/profiles/dev-<slug>-<hash>/daemon.pid` |
| Health port | `19514` | `19514 + 1 + (name_hash % 1000)` |
| Workspaces dir | `~/multica_workspaces/` | `~/multica_workspaces/` (shared; set `MULTICA_WORKSPACES_ROOT` to isolate per worktree) |
| Database | remote / production | local Docker: `multica_<slug>_<hash>` |
| Desktop profile | `desktop-api.multica.ai` | `desktop-localhost-<port>` |

Multiple worktrees can run simultaneously without conflict.

## Troubleshooting

### Missing Env File

If you see:

```text
Missing env file: .env
```

or:

```text
Missing env file: .env.worktree
```

then create the expected env file first.

Main checkout:

```bash
cp .env.example .env
```

Worktree:

```bash
make worktree-env
```

### Check Which Database a Checkout Uses

Inspect the env file:

```bash
cat .env
cat .env.worktree
```

Look for:

- `POSTGRES_DB`
- `DATABASE_URL`
- `PORT`
- `FRONTEND_PORT`

### List All Local Databases in Shared PostgreSQL

```bash
docker compose exec -T postgres psql -U multica -d postgres -At -c "select datname from pg_database order by datname;"
```

### Worktree Is Accidentally Using the Main Database

Check whether the worktree contains `.env`.

It should not.

The safe worktree setup is:

```bash
make worktree-env
make setup-worktree
make start-worktree
```

### App Stops but PostgreSQL Keeps Running

That is expected.

- `make stop`
- `make stop-main`
- `make stop-worktree`

only stop backend/frontend processes.

To stop the shared PostgreSQL container:

```bash
make db-down
```

## Destructive Reset

If you want to stop PostgreSQL and keep your local databases:

```bash
make db-down
```

If you want a fresh database for the current checkout only (drops the
database named in `POSTGRES_DB`, recreates it, and runs all migrations):

```bash
make stop        # stop backend/frontend first
make db-reset
make start
```

- only affects the current env's database; other worktree databases are untouched
- refuses to run if `DATABASE_URL` points at a remote host
- pass `ENV_FILE=.env.worktree` to target a specific worktree

If you want to wipe all local PostgreSQL data for this repo:

```bash
docker compose down -v
```

Warning:

- this deletes the shared Docker volume
- this deletes the main database and every worktree database in that volume
- after that you must run `make setup-main` or `make setup-worktree` again

## Typical Flows

### Stable Main Environment

```bash
make dev
```

### Feature Worktree

```bash
git worktree add ../multica-feature -b feat/my-change main
cd ../multica-feature
make dev
```

### Return to a Previously Configured Worktree

```bash
cd ../multica-feature
make start-worktree
```

### Validate Before Pushing

Main checkout:

```bash
make check-main
```

Worktree:

```bash
make check-worktree
```
