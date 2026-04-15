# Attachments

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Attachments subsystem handles **4 routes** and touches: auth, db.

## Routes

- `GET` `/{id}/attachments` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/attachments` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/attachments/{id}` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/attachments/{id}` params(id) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_