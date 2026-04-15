# Autopilots

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Autopilots subsystem handles **7 routes** and touches: auth, db.

## Routes

- `GET` `/api/autopilots` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/autopilots` [auth, db, upload]
  `server/cmd/server/router.go`
- `PATCH` `/api/autopilots` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/autopilots` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/autopilots/trigger` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/autopilots/runs` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/autopilots/triggers` [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_