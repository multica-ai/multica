# Multica Local — Architecture

## Overview

Multica Local is a self-contained, local-first fork of [multica-ai/multica](https://github.com/multica-ai/multica). It strips cloud dependencies and runs entirely on your machine with SQLite.

## Design Principles

1. **Local-first**: All data stays on your machine. No cloud accounts, no external services.
2. **Single-user**: One local user, auto-authenticated on startup.
3. **SQLite-backed**: All data in a single SQLite database file.
4. **Zero-config**: Run `make start` and everything works.
5. **Upstream-compatible**: Structured to minimize merge conflicts with upstream multica.

## Architecture Changes from Upstream

| Component | Upstream (multica) | Local (multica-local) |
|-----------|-------------------|----------------------|
| Database | PostgreSQL + pgx | SQLite + modernc.org/sqlite |
| Auth | Email OTP + Google OAuth | Auto-created local user + JWT |
| Storage | S3 + CloudFront | Local filesystem (~/.multica-local/storage/) |
| Daemon | Separate process, HTTP polling | Merged into server, in-process channels |
| Email | Resend API | Removed |

## Directory Structure

```
server/
├── cmd/
│   ├── server/           # HTTP API server (merged with daemon)
│   ├── multica/          # CLI tool
│   └── migrate/          # Migration runner (SQLite)
├── internal/
│   ├── auth/             # JWT auth (CloudFront signing removed)
│   ├── handler/          # HTTP handlers
│   ├── middleware/        # Auth, workspace middleware
│   ├── service/          # Business logic (email service removed)
│   ├── storage/          # Local filesystem storage (S3 removed)
│   ├── daemon/           # Agent runtime (merged into server)
│   ├── realtime/         # WebSocket hub
│   ├── events/           # In-process event bus
│   └── local/            # Local-only additions
├── pkg/
│   ├── agent/            # Claude/Codex/OpenCode CLI backends
│   ├── db/
│   │   ├── queries/      # sqlc query files (SQLite dialect)
│   │   └── generated/    # sqlc-generated Go code
│   └── protocol/         # WebSocket event types
├── migrations/           # SQLite migration files
└── go.mod
```

## Data Storage

All local data is stored under `~/.multica-local/`:

```
~/.multica-local/
├── multica.db            # SQLite database
├── storage/              # File attachments
│   └── {workspace_id}/
│       └── {attachment_id}
└── config.yaml           # Runtime configuration
```

## Upstream Sync Strategy

Local changes follow an overlay pattern to minimize merge conflicts:

- **Modified upstream files**: Marked with `// MULTICA-LOCAL` comments
- **New local-only code**: Placed in `local/` subdirectories where possible
- **Migration files**: Separate SQLite-specific migrations (not merged with upstream PG migrations)
- **Query files**: Rewritten for SQLite dialect — these diverge from upstream

See `scripts/sync-upstream.sh` for the automated merge workflow.
