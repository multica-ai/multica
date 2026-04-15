# Middleware

## auth
- auth-cookie — `apps/web/features/auth/auth-cookie.ts`
- auth.spec — `e2e/auth.spec.ts`
- auth-initializer — `packages/core/platform/auth-initializer.tsx`
- dashboard-guard — `packages/views/layout/dashboard-guard.tsx`
- use-dashboard-guard — `packages/views/layout/use-dashboard-guard.ts`
- auth — `server/internal/handler/auth.go`
- auth — `server/internal/middleware/auth.go`
- auth_test — `server/internal/middleware/auth_test.go`
- cloudfront — `server/internal/middleware/cloudfront.go`
- daemon_auth — `server/internal/middleware/daemon_auth.go`

## custom
- csp — `server/internal/middleware/csp.go`
- csp_test — `server/internal/middleware/csp_test.go`
- workspace — `server/internal/middleware/workspace.go`
- 022_task_lifecycle_guards.down — `server/migrations/022_task_lifecycle_guards.down.sql`
- 022_task_lifecycle_guards.up — `server/migrations/022_task_lifecycle_guards.up.sql`

## logging
- request_logger — `server/internal/middleware/request_logger.go`
