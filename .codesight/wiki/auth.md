# Auth

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Auth subsystem handles **6 routes** and touches: auth, db.

## Routes

- `POST` `/api/daemon/register` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/auth/send-code` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/auth/verify-code` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/auth/google` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/auth/logout` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/register` [auth, db, upload]
  `server/cmd/server/router.go`

## Middleware

- **auth-cookie** (auth) — `apps/web/features/auth/auth-cookie.ts`
- **auth.spec** (auth) — `e2e/auth.spec.ts`
- **auth-initializer** (auth) — `packages/core/platform/auth-initializer.tsx`
- **dashboard-guard** (auth) — `packages/views/layout/dashboard-guard.tsx`
- **use-dashboard-guard** (auth) — `packages/views/layout/use-dashboard-guard.ts`
- **auth** (auth) — `server/internal/handler/auth.go`
- **auth** (auth) — `server/internal/middleware/auth.go`
- **auth_test** (auth) — `server/internal/middleware/auth_test.go`
- **cloudfront** (auth) — `server/internal/middleware/cloudfront.go`
- **daemon_auth** (auth) — `server/internal/middleware/daemon_auth.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_