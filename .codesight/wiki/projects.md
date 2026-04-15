# Projects

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Projects subsystem handles **5 routes** and touches: auth, db.

## Routes

- `GET` `/api/projects/search` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/projects` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/projects` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/api/projects` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/projects` [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_