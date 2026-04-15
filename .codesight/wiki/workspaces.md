# Workspaces

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Workspaces subsystem handles **10 routes** and touches: auth, db.

## Routes

- `GET` `/api/workspaces` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/workspaces` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/workspaces/members` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/workspaces/leave` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/workspaces/invitations` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/api/workspaces` [auth, db, upload]
  `server/cmd/server/router.go`
- `PATCH` `/api/workspaces` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/workspaces/members` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/workspaces` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/workspaces/invitations/{invitationId}` params(invitationId) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_