# Workspace-id

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Workspace-id subsystem handles **1 routes** and touches: auth, db.

## Routes

- `GET` `workspace_id` [auth, db]
  `server/internal/handler/handler.go`

## Related Models

- **workspace** (5 fields) → [database.md](./database.md)
- **workspace_invitation** (8 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/internal/handler/handler.go`

---
_Back to [overview.md](./overview.md)_