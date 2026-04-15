# Agents

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Agents subsystem handles **8 routes** and touches: auth, db.

## Routes

- `GET` `/api/agents` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/agents` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/api/agents` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/agents/archive` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/agents/restore` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/agents/tasks` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/agents/skills` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/api/agents/skills` [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_