# Repository Guidelines

This file provides guidance to AI agents when working with code in this repository.

> **Single source of truth:** This file is a concise pointer document.
> All authoritative architecture, coding rules, and conventions
> live in **CLAUDE.md** at the project root. Read that file first.
> Use `Makefile`, `package.json`, and `pnpm-workspace.yaml` as the
> source of truth for the full command list.

## Quick Reference

### Architecture

Go backend + monorepo frontend (pnpm workspaces + Turborepo) with shared packages.

- `server/` - Go backend (Chi router, sqlc, gorilla/websocket)
- `apps/web/` - Next.js frontend (App Router)
- `apps/desktop/` - Electron desktop app
- `packages/core/` - Headless business logic (Zustand stores, React Query hooks, API client)
- `packages/ui/` - Atomic UI components (shadcn/Base UI, zero business logic)
- `packages/views/` - Shared business pages/components
- `packages/tsconfig/` - Shared TypeScript config

### State Management (critical)

- **React Query** owns all server state (issues, members, agents, inbox, workspace list)
- **Zustand** owns all client state (current workspace selection, view filters, drafts, modals)
- All Zustand stores live in `packages/core/` - never in `packages/views/` or app directories
- WS events invalidate React Query - never write directly to stores

### Package Boundaries (hard rules)

- `packages/core/` - zero react-dom, zero localStorage, zero process.env
- `packages/ui/` - zero `@multica/core` imports
- `packages/views/` - zero `next/*`, zero `react-router-dom`, use `NavigationAdapter` for routing
- `apps/web/platform/` - only place for Next.js APIs

### Commands

```bash
make dev              # Auto-setup + start everything
pnpm typecheck        # TypeScript check
pnpm test             # TS unit tests (Vitest)
make test             # Go tests
make check            # Full verification pipeline
```

See CLAUDE.md for the authoritative rules and common commands.

## Cursor Cloud specific instructions

The VM snapshot already has the toolchain installed: Node 22, pnpm 10.28.2, **Go 1.26.1** (at `/usr/local/go`, symlinked into `/usr/local/bin/go` so it overrides the OS `go 1.22`; verify with `go version`), and Docker Engine. The startup update script only refreshes deps (`pnpm install` + `go mod download`), so you still have to start services yourself.

Non-obvious startup caveats:

- **Docker has no init system here — start the daemon manually each session before anything that needs Postgres.** Run `sudo dockerd` in a background tmux session (e.g. session `dockerd`), wait until `docker info` succeeds, and only then run Postgres/`make dev`. If `docker ps` fails with a socket permission error in a fresh shell, run `sudo chmod 666 /var/run/docker.sock`.
- Postgres is a Docker container (`pgvector/pgvector:pg17`). `scripts/ensure-postgres.sh` / `make dev` start it via `docker compose up -d postgres`; the `pgdata` volume persists across restarts, so migrations already applied stay applied.
- Standard run flow is in the `Makefile` / `scripts/dev.sh`: `make dev` (from a checkout with `.env`) ensures Postgres, runs migrations, and starts backend (`:8080`) + web (`:3000`). To run pieces separately, source `.env` + `scripts/local-env.sh`, then `cd server && go run ./cmd/server` and `pnpm dev:web`. `go run ./cmd/server` compiles on first start (~30s) before `/health` responds.
- `.env` is gitignored; create it with `cp .env.example .env` if missing. For deterministic passwordless login in dev, set `MULTICA_DEV_VERIFICATION_CODE` (e.g. `888888`) in `.env` and restart the backend — otherwise email login codes are only printed to the backend stdout (no email provider configured locally).
- The **agent daemon** (`multica` CLI) is only needed to actually execute agent work; browsing the app and creating workspaces/issues/projects works without it.
- pnpm reports "Ignored build scripts" (sharp, msw, etc.) on install; this is expected and does not block backend, web dev, lint, typecheck, or tests.
