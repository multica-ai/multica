# Api

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Api subsystem handles **8 routes** and touches: auth, db.

## Routes

- `GET` `/{id}` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/{id}` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `PATCH` `/{id}` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/{id}` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/{runtimeId}` params(runtimeId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/{sessionId}` params(sessionId) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/{sessionId}` params(sessionId) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/{itemType}/{itemId}` params(itemType, itemId) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_