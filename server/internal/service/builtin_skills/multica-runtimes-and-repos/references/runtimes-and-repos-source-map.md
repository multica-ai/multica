# Runtimes and repos source map

<!-- CEREBRO-PATCH(runtime-access-diagnostics): FIR-4293 maps CLI and MCP to one REST contract. -->
- `server/cmd/multica/cmd_runtime.go` registers `runtime list`, `diagnostics`, `usage`, `activity`, and `update`.
- `runtime list` reads `/api/runtimes` and prints `id`, `name`, `runtime_mode`, `provider`, `status`, and `last_seen_at`.
- `runtime diagnostics` and MCP `get_runtime_access_diagnostics` read the same owner/admin-gated `/api/runtimes/{runtime-id}/access-diagnostics` contract; the report separates provider probing from MCP `tools/list` and never changes policy.
- `runtime update` posts to `/api/runtimes/{runtime-id}/update`; with `--wait` it polls update status.
- `server/cmd/multica/cmd_repo.go` registers `repo checkout <url> [--ref]`.
- `repo checkout` requires `MULTICA_DAEMON_PORT`, sends `workspace_id`, `workdir`, `ref`, `agent_name`, and `task_id` to local daemon `/repo/checkout`, then prints the checked-out path.
- `server/cmd/server/router.go` registers daemon APIs under `/api/daemon`, including workspace repos and task claim.
- `server/internal/daemon/daemon.go` claims tasks, prepares workdirs, launches provider CLIs, and reports completion.
- `server/internal/daemon/execenv/runtime_config.go` injects task/project/repo context into agent workdirs.
