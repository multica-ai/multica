# Autopilots source map

<!-- CEREBRO-PATCH(autopilot-permissions): map canonical Autopilot permission enforcement (FIR-4359). -->

- `server/cmd/multica/cmd_autopilot.go` registers `list`, `get`, `create`, `update`, `delete`, `trigger`, `runs`, `trigger-add`, `trigger-update`, `trigger-delete`, and `trigger-rotate-url`.
- The CLI maps reads/writes to `/api/autopilots`, `/api/autopilots/{id}`, `/api/autopilots/{id}/trigger`, `/api/autopilots/{id}/runs`, and trigger subroutes.
- `server/internal/service/autopilot.go` has `DispatchAutopilot`, creates `autopilot_run`, and switches on `execution_mode`.
- `create_issue` calls `dispatchCreateIssue`; `run_only` calls `dispatchRunOnly`.
- `resolveAutopilotLeader` resolves squad-assigned autopilots to the squad leader.
- `AgentReadiness` blocks archived/runtime-unready agents before enqueue.
- `server/cmd/server/router.go` exposes authenticated `/api/autopilots` routes and unauthenticated webhook ingress `/api/webhooks/autopilots/{token}`.
- `server/internal/handler/platform_capability_gate_cerebro.go` and `server/internal/cerebro/platformaction/gate.go` route member and agent mutations through the existing Permissions resolver.
- `server/internal/cerebro/platformcatalog/catalog.go` binds exact CLI/Task Mandate callables to `create_autopilot` and `trigger_autopilot`.
- `server/internal/cerebro/access/autopilot_scope.go` remains the tighten-only visibility/edit/trigger ceiling after the Permissions decision.
