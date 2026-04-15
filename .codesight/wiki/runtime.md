# Runtime

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Runtime subsystem handles **2 routes** and touches: auth, db, cache.

## Routes

- `GET` `days` [auth, db, cache]
  `server/internal/handler/runtime.go`
- `GET` `owner` [auth, db, cache]
  `server/internal/handler/runtime.go`

## Related Models

- **agent_runtime** (10 fields) → [database.md](./database.md)
- **runtime_usage** (9 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/internal/handler/runtime.go`

---
_Back to [overview.md](./overview.md)_