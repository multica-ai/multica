# Subscribers

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Subscribers subsystem handles **2 routes** and touches: auth, db.

## Routes

- `GET` `/{id}/subscribers` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/subscribers` [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_