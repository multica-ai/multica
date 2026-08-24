# Contributing Guide

This guide documents the local development workflow for contributors working on the Multica codebase.

It covers:

- first-time setup
- environments: starting, inspecting, stopping and deleting them
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

- Node.js `22`
- `pnpm` `10.28.2`
- Go `1.26.6`
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

## Environments

An environment is the database, ports, CLI profile and processes that belong to
one checkout. It is a named object: it can be listed, inspected and deleted.

```bash
make up                      # start this checkout's environment (api + web)
make up C=api,web,daemon     # choose the components
make status                  # what is running, and whether it is yours
make list                    # every environment on this machine
make down                    # stop the processes, keep the data
make destroy                 # stop, then drop the database and free the slot
make gc                      # collect expired environments or ones whose checkout is gone
```

Components are `api` (Go backend), `web` (Next.js), `daemon` (agent daemon) and
`desktop` (Electron). Selecting any of them implies `api`. `make up` is
idempotent: re-running it against a live environment reuses the database, the
profile and any component already healthy.

Three properties are worth knowing because the old flow lacked them:

- **API, Web and Desktop renderer ports, database names and profiles are allocated, not recomputed.** The
  allocator starts from this directory's path hash, so a checkout keeps the
  numbers it has always had, and moves only when the registry or a live
  listener says the slot is taken. The registry lives in `~/.multica/dev/`;
  deleting it and re-running `make up` is a supported recovery.
- **Nothing reports success for something it has not reached.** The database is
  created and verified through `DATABASE_URL` — the same string the backend
  uses — and `GET /health` reports `pid`, `commit` and `started_at` so `make up`
  can prove the process answering is the one it just started rather than a
  leftover on the same port.
- **`down` and `destroy` differ deliberately.** `down` stops processes and
  keeps the database, profile and slot, so the next `make up` is seconds.
  `destroy` consumes the database, profile, daemon task workspaces, Desktop
  userData and slot. If any deletion fails, it keeps the manifest and exits
  non-zero so cleanup can be retried instead of losing the deletion recipe.
- **Temporary environments have a best-effort fallback.** `make up
  ARGS=--ephemeral` records a 24-hour TTL. The next `make up` automatically
  collects expired and directory-less environments; `make gc` runs the same
  collection explicitly.

Run any command inside an environment's variables without repeating them:

```bash
make env-exec ARGS="-- pnpm exec playwright test"
```

`make dev` (below) still runs backend and frontend in the foreground of your
terminal, which is the right thing when you want Ctrl-C to stop everything.

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

### Removing a Worktree

Git does not provide a `pre-worktree-remove` hook. Use the repository wrapper
from another checkout so database cleanup happens before Git removes the
worktree directory:

```bash
make remove-worktree WORKTREE=../multica-feature
```

The command refuses to remove the primary checkout, the current checkout, a
locked worktree, or a worktree with uncommitted changes. If the target contains
`.env.worktree`, it shows the database name and asks for `y/N` confirmation,
drops that database, and only then runs `git worktree remove`. A worktree that
was never set up has no `.env.worktree`, so database cleanup is skipped.

Running `git worktree remove` directly bypasses this cleanup and can leave an
orphaned local database.

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

Running the complete stack — backend, frontend and daemon — from source, with
its own database and CLI profile, is one command:

```bash
make up C=api,web,daemon
```

It creates the environment if needed, sets the fixed local verification code
before the first launch, logs in as `dev@localhost`, mints a personal access
token, creates a workspace, writes the CLI profile, builds `server/bin/multica`
and starts the daemon from that binary. It then prints the URL, the login, the
commit, and the stop command.

Two constraints are enforced rather than documented:

- **The daemon runs from a built binary, never `go run`.** The daemon records
  its own executable path at startup and re-execs it as the
  execution-environment helper for every task; `go run` deletes that binary when
  the launcher exits, so the daemon would register, heartbeat, and then fail
  every task with `fork/exec …/go-build…/exe/multica: no such file or directory`.
- **`daemon start` is refused under a daemon-managed task.** A checkout below a
  `.multica/daemon_task_context.json` marker cannot start a second daemon
  competing for its own work, so `make up C=daemon` stops with that explanation
  before spending a login on it. Use `C=api,web` there.

### Desktop

```bash
make up C=desktop
```

This writes a marked `apps/desktop/.env.development.local` pointing at this
environment's backend, starts Electron with the renderer port and app name from
the environment registry, and waits until that renderer is actually serving.
Several checkouts can therefore run Desktop side by side without maintaining a
second path-derived identity. `make destroy` removes the marked env file and
this environment's Electron userData. Direct `pnpm dev:desktop` still uses its
path-derived fallback when it is run outside `make up`.

Log in with `dev@localhost` and `888888`.

### Isolation Guarantee

Nothing in this flow touches the system-installed `multica` or the default
`~/.multica/config.json`:

| Resource | System / Production | Local Dev (per environment) |
|---|---|---|
| Config | `~/.multica/config.json` | `~/.multica/profiles/dev-<slug>-<offset>/config.json` |
| Daemon PID | `~/.multica/daemon.pid` | `~/.multica/profiles/dev-<slug>-<offset>/daemon.pid` |
| Workspaces dir | `~/multica_workspaces/` | `~/multica_workspaces_dev-<slug>-<offset>/` |
| Database | remote / production | local: `multica_<slug>_<offset>` |
| Registry | — | `~/.multica/dev/envs/<name>/` |
| Desktop profile | `desktop-api.multica.ai` | `desktop-localhost-<port>` |

Multiple environments run simultaneously without conflict; `make list` shows
all of them.

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

To permanently drop the current worktree database without recreating it:

```bash
make db-drop ENV_FILE=.env.worktree
```

The command prints the selected database and environment file, then requires a
`y/N` confirmation. It only operates on the local Docker PostgreSQL service,
protects PostgreSQL system databases, and refuses to drop the default main
database `multica` unless `ALLOW_MAIN_DB_DROP=1` is explicitly supplied.
Declining the confirmation is a successful no-op; when called by
`make remove-worktree`, it also leaves the worktree in place.

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
