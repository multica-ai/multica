# Skills

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Skills subsystem handles **12 routes** and touches: auth, db.

## Routes

- `GET` `/{id}/skills` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/{id}/skills` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/skills` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/skills` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/skills/import` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/api/skills` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/skills` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/skills/files` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/api/skills/files` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/skills/files/{fileId}` params(fileId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/skills` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/skills` [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_