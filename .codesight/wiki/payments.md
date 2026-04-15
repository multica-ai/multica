# Payments

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Payments subsystem handles **4 routes** and touches: auth, db, cache, payment.

## Routes

- `POST` `/api/issues/subscribe` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/{id}/subscribe` params(id) [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/subscribe` [auth, db, upload]
  `server/cmd/server/router.go`
- `ALL` `/repo/checkout` [cache, payment]
  `server/internal/daemon/health.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`
- `server/internal/daemon/health.go`

---
_Back to [overview.md](./overview.md)_