# Activity

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Activity subsystem handles **2 routes** and touches: auth, db.

## Routes

- `GET` `/{runtimeId}/activity` params(runtimeId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/activity` [auth, db, upload]
  `server/cmd/server/router.go`

## Related Models

- **activity_log** (6 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_