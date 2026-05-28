# Server Agent Guidelines

This file provides backend-specific guidance for AI agents working in `server/`.

> **Single source of truth:** Repository-wide architecture, commands, and
> conventions live in `../CLAUDE.md`. Read that file before editing backend code.

## Backend Scope

`server/` contains the Go backend, including:

- HTTP handlers under `internal/handler/`
- sqlc query code under `pkg/db/`
- migrations under `migrations/`
- CLI and daemon entrypoints under `cmd/`

## Local Commands

Run commands from the repository root unless a specific command says otherwise.

```bash
make server      # Run the Go server only
make daemon      # Run the local daemon
make build       # Build server and CLI binaries
make test        # Run Go tests
make sqlc        # Regenerate sqlc code after query changes
make migrate-up  # Run database migrations
```

For a single Go test:

```bash
cd server && go test ./internal/handler/ -run TestName
```

## Backend Rules

- Follow standard Go conventions and run `gofmt` for code changes.
- Keep handlers in `internal/handler/` aligned with the UUID parsing convention in `../CLAUDE.md`.
- When editing SQL queries in `pkg/db/queries/`, regenerate sqlc output with `make sqlc`.
- Keep migrations paired as `.up.sql` and `.down.sql` files.
