# Tasks

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Tasks subsystem handles **14 routes** and touches: auth, db.

## Routes

- `POST` `/{id}/tasks/{taskId}/cancel` params(id, taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/{id}/tasks` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/tasks/{taskId}/status` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/tasks/{taskId}/start` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/tasks/{taskId}/progress` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/tasks/{taskId}/complete` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/tasks/{taskId}/fail` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/tasks/{taskId}/usage` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/tasks/{taskId}/messages` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/tasks/{taskId}/messages` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/tasks/{taskId}/cancel` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/tasks/{taskId}/messages` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/tasks` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/tasks/{taskId}/cancel` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_