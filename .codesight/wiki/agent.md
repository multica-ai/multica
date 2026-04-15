# Agent

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Agent subsystem handles **1 routes** and touches: auth, db, queue.

## Routes

- `GET` `include_archived` [auth, db, queue]
  `server/internal/handler/agent.go`

## Related Models

- **agent** (10 fields) → [database.md](./database.md)
- **agent_task_queue** (10 fields) → [database.md](./database.md)
- **agent_runtime** (10 fields) → [database.md](./database.md)
- **agent_skill** (2 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/internal/handler/agent.go`

---
_Back to [overview.md](./overview.md)_