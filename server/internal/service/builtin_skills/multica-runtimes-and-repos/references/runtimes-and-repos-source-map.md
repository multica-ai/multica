# Runtimes repos source map

- `server/cmd/multica/cmd_runtime.go` registers `runtime list`, `usage`,
  `activity`, `update`, `delete`.
- `runtime list` reads `/api/runtimes` and prints `id`, `name`,
  `runtime_mode`, `provider`, `status`…
- `runtime update` posts to `/api/runtimes/{runtime-id}/update`; `--wait`
  polls update progress.
- `runtime delete` deletes `/api/runtimes/{runtime-id}`; `--cascade` first
  reads the workspace's repo config then deletes.
- `server/cmd/multica/cmd_repo.go` registers `repo checkout <url> [--ref]`.
- `repo checkout` requires `MULTICA_DAEMON_PORT`; sends `workspace_id`,
  `workdir`, `ref`, `agent_id`.
- `server/internal/daemon/health.go` resolves the checkout ref: request `ref`
  wins, else the resource's default; then daemon checkout cache.
- `server/internal/daemon/daemon.go` injects `MULTICA_REPO_CHECKOUT_MODE=isolated`
  for Linux and Windows Codex tasks; Linux keeps the isolated checkout it
already had; Windows Codex now uses the same layout to cover its native
sandbox, where a linked worktree's external gitdir stays read-only and
`git add`/`git commit` fail (multica-ai/multica#6449). `server/internal/daemon/repocache/cache.go`
implements the mode as a local clone with task-local Git metadata and the
real repository as `origin`; on Windows it clones with `--no-hardlinks` so
objects are private copies, not NTFS links. Other runtimes keep the
linked-worktree path.
- `server/cmd/server/router.go` registers daemon APIs under `/api/daemon`,
  including workspace repo checkout.
- `server/internal/daemon/daemon.go` claims tasks, prepares workdirs, launches
  provider CLIs; validates the task-scoped `mat_` credential, exports
  `MULTICA_TASK_CONFIG_ROOT` ahead of custom env (agents cannot override it).
  `server/internal/daemon/execenv/execenv.go` creates/restores the private
  per-task `multica-config` dir (0700), never copying the Owner's profile;
  `server/internal/cli/config.go` resolves CLI profiles below
  `MULTICA_TASK_CONFIG_ROOT` when present. CLI boundary enforced in
  `cmd_agent.go`, `cmd_config.go`, `cmd_auth.go`, `cmd_login.go`,
  `cmd_setup.go`, `cmd_workspace.go`, `cmd_runtime_profile.go`, `cmd_daemon.go`.
  `cmd_daemon.go` scopes the read-only diagnostics: `daemonStatusHealthPort`
  takes the injected `MULTICA_DAEMON_PORT` (never `--profile` hash);
  `resolveDiskUsageRoot` takes `daemon.TaskWorkspacesRootEnv` (never
  `$HOME`-derived), `checkTaskDiskUsageScope` rejects widening flags, STATUS
  column skipped (Owner state).
- `server/internal/daemon/execenv/runtime_config.go` injects task/project/repo
  context into agent briefs.
