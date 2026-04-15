# Upload-file

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Upload-file subsystem handles **1 routes** and touches: auth, db.

## Routes

- `POST` `/api/upload-file` [auth, db, upload]
  `server/cmd/server/router.go`

## Related Models

- **skill_file** (4 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_