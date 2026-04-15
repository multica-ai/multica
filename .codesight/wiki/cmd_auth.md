# Cmd_auth

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Cmd_auth subsystem handles **3 routes** and touches: auth, db.

## Routes

- `GET` `token` [auth, db]
  `server/cmd/multica/cmd_auth.go`
- `GET` `state` [auth, db]
  `server/cmd/multica/cmd_auth.go`
- `ALL` `/callback` [auth, db]
  `server/cmd/multica/cmd_auth.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/multica/cmd_auth.go`

---
_Back to [overview.md](./overview.md)_