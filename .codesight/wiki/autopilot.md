# Autopilot

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Autopilot subsystem handles **3 routes** and touches: auth, db, payment.

## Routes

- `GET` `status` [auth, db, payment]
  `server/internal/handler/autopilot.go`
- `GET` `limit` [auth, db, payment]
  `server/internal/handler/autopilot.go`
- `GET` `offset` [auth, db, payment]
  `server/internal/handler/autopilot.go`

## Related Models

- **autopilot** (14 fields) → [database.md](./database.md)
- **autopilot_trigger** (10 fields) → [database.md](./database.md)
- **autopilot_run** (12 fields) → [database.md](./database.md)

## Source Files

Read these before implementing or modifying this subsystem:
- `server/internal/handler/autopilot.go`

---
_Back to [overview.md](./overview.md)_