# Inbox

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Inbox subsystem handles **8 routes** and touches: auth, db.

## Routes

- `GET` `/api/inbox` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/inbox/unread-count` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/inbox/mark-all-read` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/inbox/archive-all` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/inbox/archive-all-read` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/inbox/archive-completed` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/inbox/{id}/read` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/inbox/{id}/archive` params(id) [auth, db, upload]
  `server/cmd/server/router.go`

## Related Models

- **inbox_item** (11 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_