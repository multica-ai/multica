# Autopilots source map

- `server/cmd/multica/cmd_autopilot.go` registers `list`, `get`, `create`, `update`, `delete`, `trigger`, `runs`, nested `runs cancel`, `trigger-add`, `trigger-update`, `trigger-delete`, and `trigger-rotate-url` (lines 63-75, 105-158).
- The CLI maps reads/writes to `/api/autopilots`, `/api/autopilots/{id}`, `/api/autopilots/{id}/trigger`, `/api/autopilots/{id}/runs`, run-scoped `/api/autopilot-runs/{runId}/cancel`, and trigger subroutes (lines 522-545 for run cancel).
- `server/internal/service/autopilot.go` has `DispatchAutopilot`, creates `autopilot_run`, and switches on `execution_mode`.
- `create_issue` calls `dispatchCreateIssue`; `run_only` calls `dispatchRunOnly`.
- `resolveAutopilotLeader` resolves squad-assigned autopilots to the squad leader.
- `AgentReadiness` blocks archived/runtime-unready agents before enqueue.
- `server/cmd/server/router.go` exposes authenticated `/api/autopilots` routes, authenticated `/api/autopilot-runs/{runId}/cancel` (line 796), and unauthenticated webhook ingress `/api/webhooks/autopilots/{token}`.
- `server/internal/handler/autopilot.go` `CancelAutopilotRun` enforces workspace scoping, private agent/squad-leader access, terminal idempotency, linked task cancellation, and a structured response (lines 1507-1628).
- `server/pkg/db/queries/autopilot.sql` `CancelAutopilotRun` marks active runs `cancelled` with `completed_at` and failure reason while leaving terminal runs untouched (lines 230-237).
