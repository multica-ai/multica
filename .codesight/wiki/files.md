# Files

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Files subsystem handles **6 routes** and touches: auth, db.

## Routes

- `GET` `/{id}/files` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/{id}/files` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/{id}/files/{fileId}` params(id, fileId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/files` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/files` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/files/{fileId}` params(fileId) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_