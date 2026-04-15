# Comments

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Comments subsystem handles **8 routes** and touches: auth, db.

## Routes

- `POST` `/{id}/comments` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/{id}/comments` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/api/comments/{commentId}` params(commentId) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/comments/{commentId}` params(commentId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/comments/{commentId}/reactions` params(commentId) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/comments/{commentId}/reactions` params(commentId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/comments` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/comments` [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_