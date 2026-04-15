# Task-runs

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Task-runs subsystem handles **2 routes** and touches: auth, db.

## Routes

- `GET` `/{id}/task-runs` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/task-runs` [auth, db, upload]
  `server/cmd/server/router.go`

## Related Models

- **agent_task_queue** (10 fields) → [database.md](./database.md)
- **task_message** (8 fields) → [database.md](./database.md)
- **task_usage** (8 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_