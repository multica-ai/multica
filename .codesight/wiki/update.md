# Update

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Update subsystem handles **4 routes** and touches: auth, db.

## Routes

- `POST` `/{runtimeId}/update` params(runtimeId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/{runtimeId}/update/{updateId}` params(runtimeId, updateId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/update` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/update/{updateId}` params(updateId) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_