# Triggers

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Triggers subsystem handles **4 routes** and touches: auth, db.

## Routes

- `POST` `/{id}/triggers` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `PATCH` `/triggers/{triggerId}` params(triggerId) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/triggers/{triggerId}` params(triggerId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/triggers` [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_