# Tokens

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Tokens subsystem handles **3 routes** and touches: auth, db.

## Routes

- `GET` `/api/tokens` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/tokens` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/tokens/{id}` params(id) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_