# Trigger

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Trigger subsystem handles **2 routes** and touches: auth, db.

## Routes

- `POST` `/{id}/trigger` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/trigger` [auth, db, upload]
  `server/cmd/server/router.go`

## Related Models

- **autopilot_trigger** (10 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_