# Issue

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Issue subsystem handles **8 routes** and touches: auth, db, queue.

## Routes

- `GET` `q` [auth, db, queue, upload]
  `server/internal/handler/issue.go`
- `GET` `include_closed` [auth, db, queue, upload]
  `server/internal/handler/issue.go`
- `GET` `priority` [auth, db, queue, upload]
  `server/internal/handler/issue.go`
- `GET` `assignee_id` [auth, db, queue, upload]
  `server/internal/handler/issue.go`
- `GET` `assignee_ids` [auth, db, queue, upload]
  `server/internal/handler/issue.go`
- `GET` `creator_id` [auth, db, queue, upload]
  `server/internal/handler/issue.go`
- `GET` `project_id` [auth, db, queue, upload]
  `server/internal/handler/issue.go`
- `GET` `open_only` [auth, db, queue, upload]
  `server/internal/handler/issue.go`

## Related Models

- **issue** (14 fields) → [database.md](./database.md)
- **issue_label** (4 fields) → [database.md](./database.md)
- **issue_to_label** (2 fields) → [database.md](./database.md)
- **issue_dependency** (4 fields) → [database.md](./database.md)
- **issue_subscriber** (4 fields) → [database.md](./database.md)
- **issue_reaction** (6 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/internal/handler/issue.go`

---
_Back to [overview.md](./overview.md)_