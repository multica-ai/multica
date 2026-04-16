# Local Development (reusing existing PostgreSQL)

This setup runs Multica against a pre-existing Postgres container on the host
(e.g. `unitesync-postgres`) instead of the bundled `pgvector/pg17` one in
`docker-compose.yml`, so there's no port-5432 collision.

## Architecture

Three moving parts:

1. **PostgreSQL** — an existing host container (e.g. `unitesync-postgres`) on port 5432.
2. **Backend + frontend** — Docker, via `docker-compose.local.yml`.
3. **Daemon** — runs on the **host** (not Docker), executes agents using the local `claude` CLI and `~/.claude/` credentials.

## One-time setup

Already done on this machine — listed for reference.

```bash
# 1. Ensure the multica database exists in the host Postgres
docker exec unitesync-postgres psql -U postgres -c 'CREATE DATABASE multica;'

# 2. Point .env at the host Postgres
#    DATABASE_URL=postgres://postgres:postgres@localhost:5432/multica?sslmode=disable
#    POSTGRES_USER=postgres
#    POSTGRES_PASSWORD=postgres
#    POSTGRES_DB=multica

# 3. Apply migrations
cd /Users/daffaarifadilah/unitesync/multica
set -a && source .env && set +a
cd server && go run ./cmd/migrate up

# 4. Authenticate the daemon against the local backend (only once)
cd /Users/daffaarifadilah/unitesync/multica
go run ./server/cmd/multica login --profile local
```

## Daily use — the easy way

Use `scripts/local.sh`:

```bash
./scripts/local.sh up      # starts Postgres + containers + daemon
./scripts/local.sh down    # stops everything
./scripts/local.sh status  # quick health check
./scripts/local.sh logs    # tails backend logs
./scripts/local.sh rebuild # rebuilds backend+frontend images (after code changes)
./scripts/local.sh migrate # applies any new migrations
```

Then open http://localhost:3000.

## Daily use — the manual way

```bash
# Start
docker start unitesync-postgres
docker compose -f docker-compose.local.yml up -d
cd /Users/daffaarifadilah/unitesync/multica && make daemon

# Stop
docker compose -f docker-compose.local.yml down
go run ./server/cmd/multica daemon stop --profile local
# (unitesync-postgres stays running for other projects)
```

## Native dev (faster iteration)

For active backend/frontend dev, skip Docker and run natively — HMR is much faster:

```bash
# Backend
cd /Users/daffaarifadilah/unitesync/multica
set -a && source .env && set +a
cd server && go run ./cmd/server

# Frontend (separate terminal)
pnpm dev:web
```

Daemon still runs on the host with `make daemon`.

## After pulling new code

```bash
./scripts/local.sh migrate  # if migrations changed
./scripts/local.sh rebuild  # rebuilds and restarts containers
```

## Checks and logs

```bash
docker compose -f docker-compose.local.yml ps
docker compose -f docker-compose.local.yml logs -f backend
docker compose -f docker-compose.local.yml logs -f frontend
tail -f ~/.multica/profiles/local/daemon.log
multica daemon status --profile local
```

## RAM footprint (idle)

| Component          | RAM    |
| ------------------ | ------ |
| frontend container | ~89 MB |
| backend container  | ~38 MB |
| unitesync-postgres | ~58 MB |
| daemon (host)      | ~16 MB |
| **Total idle**     | **~200 MB** |

Each concurrent `claude` subprocess the daemon spawns adds ~50–150 MB plus Claude Code's own overhead. Expect spikes to 500 MB – 1 GB while agents actively run.

## Files added for this setup

- `docker-compose.local.yml` — backend + frontend only; backend reaches host Postgres via `host.docker.internal:5432`.
- `scripts/local.sh` — start/stop/status/logs/rebuild/migrate wrapper.
- `LOCAL_DEV.md` — this document.
