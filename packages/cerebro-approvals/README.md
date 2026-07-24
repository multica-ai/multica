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

## Platform-action asks

The `create_issue` server floor is always active for agents. An Ask creates or
rejoins one pending row without writing an issue. REST returns HTTP 202; the CLI
polls and retries the identical body with `X-Platform-Approval-ID`. Workspace
MCP and Firtal Gateway wait in-process. On approval, the server atomically
consumes the exact workspace + agent + capability + resource + approval ID
match before allowing one mutation. Rejected, expired, mismatched, and replayed
one-shot approvals remain non-mutating. A time-boxed period grant is explicitly
marked reusable and remains valid only until its expiry.

Inline cards and approval realtime subscriptions are inert when
both `cerebro_approvals` and `cerebro_approval_gate` are off. The presentation
flag can hide the inbox only when Ask enforcement is also off; while the gate is
active, navigation, inline cards and realtime updates remain visible so every
Ask has a human decision path. The server floor remains
active regardless of the flag.
