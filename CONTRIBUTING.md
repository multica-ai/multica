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

## Contribution Terms

By submitting a contribution to Multica — a pull request, a patch, or any
other work — you agree to condition 2 of the [Multica License](LICENSE):

- your contribution is submitted under the Multica License as a whole (the
  additional conditions in Part I together with the incorporated Apache
  License 2.0 text in Part II), not under the Apache License 2.0 alone;
- your contributed code may be used for commercial purposes, including the
  producer's cloud business operations;
- the producer can adjust the Multica License to be more strict or relaxed
  as deemed necessary.

See the [LICENSE](LICENSE) file for the full terms.

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
make dev-bootstrap
```

This takes a clean checkout all the way to a usable environment: everything
`make dev` does, plus a logged-in user, a workspace, a CLI profile, and a
running daemon — then prints the URL, the login, and how to stop it. Stop it
with `make dev-bootstrap-stop`. See
[Full-Stack Isolated Testing](#full-stack-isolated-testing) for the details.

If you only want backend + frontend in the foreground:

```bash
make dev
```

This single command:

- auto-detects whether you're in a main checkout or a worktree
- creates the appropriate env file (`.env` or `.env.worktree`) if it doesn't exist
- checks that prerequisites (Node.js, pnpm, Go, Docker) are installed
- refuses to start if the backend or frontend port is already served, instead of
  dying on the bind while the old instance keeps answering
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
make dev-bootstrap
make dev-bootstrap-stop
make test
make migrate-up
make migrate-down
```

These generic targets require a valid env file in the current directory
(`make dev` and `make dev-bootstrap` create one if it is missing).

`make stop` terminates only the processes **listening** on this checkout's
backend and frontend ports, with `TERM` before `KILL`. It does not touch clients
of those ports — notably the local daemon, which holds a long-lived connection
to the backend and used to be killed along with it.

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
make daemon                                    # restart --profile local
make daemon ARGS="start --profile my-profile"  # any daemon subcommand
```

`make daemon` builds `server/bin/multica` first and runs the daemon from that
binary — never through `go run`. See
[Why the daemon must not run through `go run`](#why-the-daemon-must-not-run-through-go-run).

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

From the checkout root:

```bash
make dev-bootstrap
```

That is the whole flow. It runs, in order, the twelve steps that used to be
manual — and where the order matters, it is the reason this is a script:

1. pins a durable `TMPDIR` (an agent's `TMPDIR` is deleted when its run ends)
2. generates `.env` / `.env.worktree` with this directory's ports and database
3. sets `MULTICA_DEV_VERIFICATION_CODE=888888` **before** the first launch, so
   there is no start → edit → restart detour
4. refuses to continue if either port is already served (see Troubleshooting)
5. creates the database against `DATABASE_URL` rather than `docker exec`, which
   is what makes it work when a native PostgreSQL owns 5432
6. starts backend + frontend detached, in their own process group
7. waits for `/health` and verifies the answering process is the one it started
8. `send-code` once, `verify-code` once (retries lock the code out), then mints
   a personal access token
9. creates the workspace and marks onboarding complete
10. writes `~/.multica/profiles/<profile>/config.json`
11. builds `server/bin/multica` and starts the daemon from that binary
12. prints the URL, the login, the profile, the logs, and the stop command

Re-running it against an existing environment is safe: the env file, database,
workspace, and profile are reused.

Overrides, if you need them: `MULTICA_DEV_EMAIL`, `MULTICA_DEV_WORKSPACE_NAME`,
`MULTICA_DEV_WORKSPACE_SLUG`, `MULTICA_DEV_TMPDIR`.

#### Why the daemon must not run through `go run`

`make dev-bootstrap` builds `server/bin/multica` and starts the daemon from it,
and `make daemon` does the same. This is not a preference.

The daemon records its own executable path at startup and re-execs it as the
execution-environment preparation helper for **every task**. Under `go run` that
path is a temp build which the toolchain deletes as soon as the `go run` parent
exits — so the daemon starts, registers, heartbeats, and then fails every task
with `fork/exec /…/go-build…/exe/multica: no such file or directory`. `daemon
start` now refuses such a binary up front rather than deferring the failure.

One-shot CLI commands through `make cli` (`go run`) are unaffected — nothing
records a path that has to outlive the process.

### Stop the Isolated Environment

```bash
make dev-bootstrap-stop
```

Stops the daemon, the backend, and the frontend. PostgreSQL, the database, the
workspace, and the CLI profile are deliberately left in place — they are what
makes the next `make dev-bootstrap` fast.

`make stop` / `make stop-worktree` stop only the backend and frontend listeners
and deliberately leave a running daemon alive.

For a full reset:

```bash
make db-reset                                  # drop + recreate this env's database
make clean                                     # build artifacts
rm -rf "$HOME/.multica/profiles/<profile>"     # CLI profile (printed by bootstrap)
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

#### Running multiple worktrees side-by-side

`pnpm dev:desktop` auto-isolates a worktree so several worktrees can run their
own desktop dev instance at once — no extra setup. From a linked worktree it
derives, from the worktree path (same `cksum % 1000` offset as the backend /
frontend ports in `.env.worktree`):

- `DESKTOP_RENDERER_PORT` = `5174 + offset` — its own Vite dev server (`5174`
  base leaves `5173` for the primary checkout, even when `offset` is `0`). The
  one offset that would land on `6000` gets `6174` instead: Chromium treats
  `6000` as a restricted port and fails the load with `ERR_UNSAFE_PORT`
- `DESKTOP_APP_SUFFIX` = `<folder>-<offset>` — its own single-instance lock /
  `userData`, and an app named `Multica Canary <folder>-<offset>` so it is
  distinguishable in Cmd+Tab. The offset keeps it unique across worktrees that
  share a folder name at different paths.

The primary checkout is left untouched (`5173`, `Multica Canary`). Set either
env var explicitly to override the derived value. Which backend each instance
talks to is still controlled only by `apps/desktop/.env*` above — point each
worktree's desktop at its own backend to also isolate the daemon profile.

### Isolation Guarantee

Nothing in this flow touches the system-installed `multica` or the default
`~/.multica/config.json`:

| Resource | System / Production | Local Dev (per-worktree) |
|---|---|---|
| Config | `~/.multica/config.json` | `~/.multica/profiles/dev-<slug>-<hash>/config.json` |
| Daemon PID | `~/.multica/daemon.pid` | `~/.multica/profiles/dev-<slug>-<hash>/daemon.pid` |
| Health port | `19514` | `19514 + 1 + (name_hash % 1000)` |
| Workspaces dir | `~/multica_workspaces/` | `~/multica_workspaces_dev-<slug>-<hash>/` |
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

### Port Already in Use

`make dev` / `make dev-bootstrap` refuse to start when the backend or frontend
port already has a listener, and print which process owns it and when it
started. Take the message literally — this is the failure that used to be
invisible: the new process dies on the bind, the **old** one keeps serving, and
`/health` answers 200 throughout, so a "restart" looks successful while the
previous build handles every request.

```bash
make stop            # main checkout
make stop-worktree   # worktree checkout
```

### Am I Talking to the Process I Just Started?

`GET /health` identifies the process that answered:

```console
$ curl -s http://localhost:8080/health
{"status":"ok","commit":"d09019cd9","started_at":"2026-08-07T08:30:04Z","pid":35518}
```

If `started_at` predates your restart, you are talking to a leftover instance —
a 200 alone never proves otherwise. `commit` is populated by `make dev`,
`make start`, and `make build`; it stays `unknown` for a bare
`go run ./cmd/server`.

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
