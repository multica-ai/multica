# Invitations

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Invitations subsystem handles **8 routes** and touches: auth, db.

## Routes

- `GET` `/{id}/invitations` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/{id}/invitations/{invitationId}` params(id, invitationId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/invitations` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/invitations/{invitationId}` params(invitationId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/invitations` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/invitations/{id}` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/invitations/{id}/accept` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/invitations/{id}/decline` params(id) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_