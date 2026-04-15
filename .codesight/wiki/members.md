# Members

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Members subsystem handles **6 routes** and touches: auth, db.

## Routes

- `GET` `/{id}/members` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/{id}/members` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `PATCH` `/members/{memberId}` params(memberId) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/members/{memberId}` params(memberId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/members` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/members` [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_