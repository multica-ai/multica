# @multica/cerebro-approvals

Approval inbox for the Multica core permissions stack (FIR-2131, phase 3).

When the permission engine (`server/internal/cerebro/permissions`) returns
`needs_approval` for an `(actor, capability, resource)` request, the enforcement
gate calls `approvals.Service.Intake` to materialise the pending ask. This
package renders the human side: one inbox list of pending/decided asks, an
ask-detail sheet, and approve / reject / delegate actions — each writing an
append-only audit row.

## Layout

- `core/` — headless logic: zod schemas (`types.ts`), API client over
  `api.cerebroRequest` (`api.ts`), React Query options (`queries.ts`),
  mutations (`mutations.ts`).
- `views/` — `ApprovalsPage` (inbox + WS-driven toast on new asks) and
  `ApprovalsNavItem` (sidebar entry with pending-count badge).

## Backend

Service, HTTP handler, SQL and migration live in
`server/internal/cerebro/approvals` + `server/migrations/9037_cerebro_approvals.*`.

Endpoints (mounted in `server/cmd/server/router.go`):

| Method | Path | Access |
|---|---|---|
| GET | `/api/workspaces/{id}/approvals` | member |
| GET | `/api/workspaces/{id}/approvals/audit` | member |
| GET | `/api/workspaces/{id}/approvals/{approvalId}` | member |
| POST | `/api/workspaces/{id}/approvals/intake` | admin/owner (seam) |
| POST | `/api/workspaces/{id}/approvals/{approvalId}/approve` | admin/owner |
| POST | `/api/workspaces/{id}/approvals/{approvalId}/reject` | admin/owner |
| POST | `/api/workspaces/{id}/approvals/{approvalId}/delegate` | admin/owner |

Terminal decisions are race-safe: only a still-`pending` row transitions, so two
concurrent approvers can't both win — the loser gets HTTP 409.

## Feature flag

Gated by `cerebro_approvals` (default off until browser-verified + deployed).

## Status

The live gate wiring (calling `Service.Intake` from the runtime/MCP enforcement
path) lands once phase 2's enforcement is wired in; until then the admin-only
`/approvals/intake` endpoint exercises the full inbox flow end-to-end.
