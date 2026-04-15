# Runtimes

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Runtimes subsystem handles **13 routes** and touches: auth, db.

## Routes

- `GET` `/api/runtimes` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/runtimes/usage` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/runtimes/activity` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/runtimes/ping` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/runtimes/ping/{pingId}` params(pingId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/runtimes/update` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/runtimes/update/{updateId}` params(updateId) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/runtimes` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/runtimes/{runtimeId}/tasks/claim` params(runtimeId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/runtimes/{runtimeId}/tasks/pending` params(runtimeId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/runtimes/{runtimeId}/usage` params(runtimeId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/runtimes/{runtimeId}/ping/{pingId}/result` params(runtimeId, pingId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/runtimes/{runtimeId}/update/{updateId}/result` params(runtimeId, updateId) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_