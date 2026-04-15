# Usage

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Usage subsystem handles **5 routes** and touches: auth, db.

## Routes

- `GET` `/{id}/usage` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/usage/daily` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/usage/summary` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/{runtimeId}/usage` params(runtimeId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/usage` [auth, db, upload]
  `server/cmd/server/router.go`

## Related Models

- **runtime_usage** (9 fields) → [database.md](./database.md)
- **task_usage** (8 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_