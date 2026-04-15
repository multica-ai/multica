# Cmd_issue

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Cmd_issue subsystem handles **1 routes** and touches: auth.

## Routes

- `GET` `X-Total-Count` [auth, upload]
  `server/cmd/multica/cmd_issue.go`

## Related Models

- **issue** (14 fields) → [database.md](./database.md)
- **issue_label** (4 fields) → [database.md](./database.md)
- **issue_to_label** (2 fields) → [database.md](./database.md)
- **issue_dependency** (4 fields) → [database.md](./database.md)
- **issue_subscriber** (4 fields) → [database.md](./database.md)
- **issue_reaction** (6 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/multica/cmd_issue.go`

---
_Back to [overview.md](./overview.md)_