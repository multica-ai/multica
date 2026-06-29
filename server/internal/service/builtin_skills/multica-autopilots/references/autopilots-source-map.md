# Autopilots source map

- `server/cmd/multica/cmd_autopilot.go` registers `list`, `get`, `create`, `update`, `delete`, `trigger`, `runs`, `trigger-add`, `trigger-update`, `trigger-delete`, and `trigger-rotate-url`.
- The CLI maps reads/writes to `/api/autopilots`, `/api/autopilots/{id}`, `/api/autopilots/{id}/trigger`, `/api/autopilots/{id}/runs`, and trigger subroutes.
- `server/internal/service/autopilot.go` has `DispatchAutopilot`, creates `autopilot_run`, and switches on `execution_mode`.
- `create_issue` calls `dispatchCreateIssue`; `run_only` calls `dispatchRunOnly`.
- `resolveAutopilotLeader` resolves squad-assigned autopilots to the squad leader.
- `AgentReadiness` blocks archived/runtime-unready agents before enqueue.
- `server/cmd/server/router.go` exposes authenticated `/api/autopilots` routes and unauthenticated webhook ingress `/api/webhooks/autopilots/{token}`.
- Write/execute authorization lives in `canWriteAutopilot` / `requireAutopilotWrite` (`server/internal/handler/autopilot.go`): editing, deleting, triggering, replaying deliveries, and managing triggers/webhook secrets require the autopilot's creator or a workspace owner/admin. Reads (list/get/runs/deliveries) stay open to any workspace member, but `GetAutopilot` redacts `webhook_token`/`webhook_path`/`webhook_url` for callers who lack write access, since the token alone can trigger the autopilot. Creating a new autopilot is still open to any member (they become its creator). This is the autopilot-level View/Write layer; it is independent of, and ANDed with, the private-assignee-agent gate enforced at dispatch time in `shouldSkipDispatch`.
