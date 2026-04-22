# Architecture Notes — Agent Execution Seams

> Read-only audit produced on 2026-04-22. All file references are relative to
> the repo root. Line ranges are from the state of the `claude/architecture-review-fxUkI`
> branch at the time of this audit.
>
> **No PLAN.md was found in this repository.** The "Risks and surprises"
> section therefore notes things that contradict common assumptions or the
> design described in README.md and CLI_AND_DAEMON.md rather than a specific
> plan document.

---

## Seam 1 — Agent Launch Path

### 1a. Issue assignment → task enqueue (server side)

When a user assigns an issue to an agent, the HTTP handler for issue updates
detects `assignee_type == "agent"` and calls:

```go
// server/internal/handler/issue.go  (lines ~925, 1122, 1132, 1430, 1438)
h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
```

`EnqueueTaskForIssue` looks up the agent record, validates it has a runtime,
then inserts a row into `agent_task_queue` with `status = 'queued'`:

```go
// server/internal/service/task.go:63-73
task, err := s.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
    AgentID:          issue.AssigneeID,
    RuntimeID:        agent.RuntimeID,
    IssueID:          issue.ID,
    Priority:         priorityToInt(issue.Priority),
    TriggerCommentID: commentID,
})
```

**Hook point for extension:** `TaskService.EnqueueTaskForIssue` at
`server/internal/service/task.go:38`. Any pre-dispatch logic (budget checks,
scheduling) belongs here.

### 1b. Daemon main loop and task discovery

The daemon is the binary at `server/internal/daemon/`. Its main entry point is:

```go
// server/internal/daemon/daemon.go:88-135
func (d *Daemon) Run(ctx context.Context) error { ... }
```

`Run` starts four goroutines: `workspaceSyncLoop`, `heartbeatLoop`, `gcLoop`,
`serveHealth`, then blocks on `pollLoop`.

**The daemon discovers tasks via HTTP polling — not WebSocket.** It calls
`ClaimTask` on every runtime it has registered, at `PollInterval` (default 3 s):

```go
// server/internal/daemon/daemon.go:775-812
rid := runtimeIDs[(pollOffset+i)%n]
task, err := d.client.ClaimTask(ctx, rid)
// ClaimTask → POST /api/daemon/runtimes/{runtimeId}/tasks/claim
```

When a task is claimed, it is dispatched in a goroutine bounded by a semaphore
of size `MaxConcurrentTasks` (default 20):

```go
// server/internal/daemon/daemon.go:800-809
go func(t Task) {
    defer wg.Done()
    defer d.activeTasks.Add(-1)
    defer func() { <-sem }()
    d.handleTask(ctx, t)
}(*task)
```

### 1c. Function that spawns the agent CLI subprocess

The call chain from polling to subprocess is:

```
pollLoop → handleTask (daemon.go:832)
         → runTask   (daemon.go:951)
         → agent.New(provider, cfg) → claudeBackend / codexBackend / ...
         → backend.Execute(ctx, prompt, opts)
         → exec.CommandContext(runCtx, execPath, args...)
```

For Claude Code specifically (`server/pkg/agent/claude.go:61`):

```go
// server/pkg/agent/claude.go:37-91
args := buildClaudeArgs(opts, b.cfg.Logger)
// args always include: -p --output-format stream-json --input-format stream-json
//   --verbose --strict-mcp-config --permission-mode bypassPermissions
cmd := exec.CommandContext(runCtx, execPath, args...)
cmd.Dir = opts.Cwd       // set to env.WorkDir
cmd.Env = buildEnv(b.cfg.Env)
stdout, _ := cmd.StdoutPipe()
cmd.Stderr = newLogWriter(b.cfg.Logger, "[claude:stderr] ")
cmd.Start()
```

**Claude always runs with `--permission-mode bypassPermissions`** — all tool
calls are auto-approved. There is no per-tool confirmation.

**The natural hook for "I want to intercept or wrap the subprocess call"** is
`agent.Backend.Execute()` defined in `server/pkg/agent/agent.go:17`. Every
provider implements this interface. Substituting the backend (via `agent.New`)
is the least-invasive extension point.

**Files examined:**
- `server/internal/daemon/daemon.go:743-830` (pollLoop)
- `server/internal/daemon/daemon.go:832-949` (handleTask)
- `server/internal/daemon/daemon.go:951-1223` (runTask)
- `server/pkg/agent/claude.go:22-211` (claudeBackend.Execute)
- `server/pkg/agent/agent.go:96-126` (agent.New factory)

### 1d. Working directory — how it is chosen

```go
// server/internal/daemon/daemon.go:993-1010
if task.PriorWorkDir != "" {
    env = execenv.Reuse(task.PriorWorkDir, provider, codexVersion, taskCtx, d.logger)
}
if env == nil {
    env, err = execenv.Prepare(execenv.PrepareParams{
        WorkspacesRoot: d.cfg.WorkspacesRoot,   // default ~/multica_workspaces
        WorkspaceID:    task.WorkspaceID,
        TaskID:         task.ID,
        ...
    }, d.logger)
}
```

`Prepare` creates:
```
{WorkspacesRoot}/{workspaceID}/{taskIDShort}/
    workdir/          ← agent Cwd
    output/
    logs/
```

**The workdir starts completely empty.** No repository is cloned into it at
preparation time. The `repos` field carried in the task payload is metadata
only (a list of URLs and descriptions).

Repos are checked out **on demand** by the agent during execution via:
```bash
multica repo checkout <url>
```

That CLI command (`server/cmd/multica/cmd_repo.go:32-92`) sends a request to
the daemon's local health server (`http://127.0.0.1:{MULTICA_DAEMON_PORT}/repo/checkout`).
The daemon handler (`server/internal/daemon/health.go:121-175`) calls
`repoCache.CreateWorktree()`, which:

1. Looks up the bare clone at `{WorkspacesRoot}/.repos/{workspaceID}/{hash}/`
2. If not present, runs `git clone --bare <url>` 
3. Runs `git fetch origin` to update
4. Runs `git worktree add -b {agentName}/{taskID} {workDir} origin/main`

**There is no automatic git checkout — the agent must call `multica repo
checkout` explicitly.** If the agent does not call it, the workdir remains
empty for the entire run.

**Files examined:**
- `server/internal/daemon/execenv/execenv.go:70-127` (Prepare)
- `server/internal/daemon/execenv/execenv.go:129-165` (Reuse)
- `server/internal/daemon/execenv/git.go:71-92` (setupGitWorktree)
- `server/internal/daemon/repocache/cache.go:1-100` (bare clone cache)
- `server/cmd/multica/cmd_repo.go:32-92` (CLI command)
- `server/internal/daemon/health.go:121-175` (health server handler)

### 1e. stdout/stderr capture and streaming

```go
// server/pkg/agent/claude.go:69-86
stdout, _ := cmd.StdoutPipe()
cmd.Stderr = newLogWriter(b.cfg.Logger, "[claude:stderr] ")
```

- **stdout** is parsed as newline-delimited JSON (Claude SDK stream format).
- **stderr** goes to the daemon's slog logger only — it is NOT forwarded to
  the server or visible in the frontend task stream.

The parsed messages are channelled through `session.Messages`. The daemon's
`executeAndDrain` goroutine (`daemon.go:1244-1376`) batches them and calls
`d.client.ReportTaskMessages()` every 500 ms, which hits
`POST /api/daemon/tasks/{id}/messages`. The server stores each message in
`task_message` and broadcasts it via WebSocket so the frontend can render
live tool-call output.

**Files examined:**
- `server/internal/daemon/daemon.go:1225-1386` (executeAndDrain)
- `server/internal/handler/daemon.go:1019-1086` (ReportTaskMessages handler)

---

## Seam 2 — Config Assembly

### 2a. Agent config stored server-side

All agent configuration lives in the `agent` table. Columns were added
incrementally across migrations:

| Column | Migration | Type | Purpose |
|--------|-----------|------|---------|
| `runtime_config` | 001 | `JSONB` | Provider + runtime mode (largely superseded) |
| `instructions` | 021 | `TEXT` | Agent identity / persona instructions |
| `custom_env` | 040 | `JSONB` | User-configurable environment variables |
| `custom_args` | 041 | `JSONB` | Extra CLI arguments appended to agent command |
| `mcp_config` | 046 | `JSONB` | MCP server configuration |
| `model` | 050 | `TEXT` | Per-agent model override |

The `workspace` table carries a `repos JSONB` column (not a separate table)
holding `[{url, description}, ...]`. Repo additions/removals are not
individually tracked.

### 2b. Config delivery to the daemon

**Config is delivered inline with the task, fetched fresh from the database on
every claim.** There is no daemon-side cache of agent config.

```go
// server/internal/handler/daemon.go:604-633
if agent, err := h.Queries.GetAgent(r.Context(), task.AgentID); err == nil {
    skills := h.TaskService.LoadAgentSkills(r.Context(), task.AgentID)
    // unmarshal custom_env, custom_args, mcp_config ...
    resp.Agent = &TaskAgentData{
        ID:           uuidToString(agent.ID),
        Name:         agent.Name,
        Instructions: agent.Instructions,
        Skills:       skills,
        CustomEnv:    customEnv,
        CustomArgs:   customArgs,
        McpConfig:    mcpConfig,
        Model:        agent.Model.String,
    }
}
```

### 2c. Files materialized on disk before the CLI launches

The following files are written by `execenv.Prepare` / `execenv.InjectRuntimeConfig`
before the agent subprocess starts:

| File | Written by | Purpose |
|------|-----------|---------|
| `{workDir}/.agent_context/issue_context.md` | `context.go:writeContextFiles` | Minimal task context (issue ID, trigger type, skill list) |
| `{workDir}/CLAUDE.md` | `runtime_config.go:InjectRuntimeConfig` | Meta skill: identity, CLI commands, workflow, repo list |
| `{workDir}/AGENTS.md` | same (for codex/copilot/opencode/openclaw/pi/cursor/kimi) | Same content, provider-specific file name |
| `{workDir}/GEMINI.md` | same (for gemini) | Same content |
| `{workDir}/.claude/skills/{name}/SKILL.md` | `context.go:writeSkillFiles` | Per-skill content (Claude uses `.claude/skills/`) |
| `{workDir}/.github/skills/{name}/SKILL.md` | same | Copilot path |
| `{workDir}/.config/opencode/skills/...` | same | OpenCode path |
| `{workDir}/.pi/agent/skills/...` | same | Pi path |
| `{workDir}/.cursor/skills/...` | same | Cursor path |
| `{workDir}/.kimi/skills/...` | same | Kimi path |
| `{taskRoot}/codex-home/` | `execenv.go:prepareCodexHomeWithOpts` | Separate CODEX_HOME for Codex (skills + sandbox config) |
| `os.TempDir()/multica-mcp-*.json` | `claude.go:writeMcpConfigToTemp` | MCP server config (temp file, not in workdir) |

**Files examined:**
- `server/internal/daemon/execenv/context.go:22-48` (writeContextFiles)
- `server/internal/daemon/execenv/runtime_config.go:22-36` (InjectRuntimeConfig)
- `server/pkg/agent/claude.go:40-58` (MCP config temp file)

### 2d. Secret injection — env vars vs config files

Secrets are injected via **environment variables only**. No secret is written
to a file on disk.

The daemon builds an `agentEnv` map before spawning the subprocess:

```go
// server/internal/daemon/daemon.go:1025-1060
agentEnv := map[string]string{
    "MULTICA_TOKEN":        d.client.Token(),  // daemon's personal access token
    "MULTICA_SERVER_URL":   d.cfg.ServerBaseURL,
    "MULTICA_DAEMON_PORT":  fmt.Sprintf("%d", d.cfg.HealthPort),
    "MULTICA_WORKSPACE_ID": task.WorkspaceID,
    "MULTICA_AGENT_NAME":   agentName,
    "MULTICA_AGENT_ID":     task.AgentID,
    "MULTICA_TASK_ID":      task.ID,
}
// agent's custom_env (e.g. ANTHROPIC_API_KEY) is merged here:
for k, v := range task.Agent.CustomEnv {
    if isBlockedEnvKey(k) { continue }
    agentEnv[k] = v
}
```

`isBlockedEnvKey` prevents overriding `MULTICA_*`, `HOME`, `PATH`, `USER`,
`SHELL`, `TERM`, `CODEX_HOME`.

`buildEnv` (`claude.go:463-480`) merges the process's own `os.Environ()` with
the extra map, stripping `CLAUDECODE` / `CLAUDE_CODE_*` vars to prevent
the inner Claude session from inheriting the outer session's state.

**The user's API key (e.g. `ANTHROPIC_API_KEY`) must be stored in
`agent.custom_env` in the database and reaches the subprocess as a plain env
var.** It is never written to a file.

---

## Seam 3 — Usage / Cost Reporting

### 3a. Token counts are parsed today

Yes, token counts are parsed from LLM responses. For Claude, the parsing
happens in `handleAssistant`:

```go
// server/pkg/agent/claude.go:219-227
if content.Usage != nil && content.Model != "" {
    u := usage[content.Model]
    u.InputTokens  += content.Usage.InputTokens
    u.OutputTokens += content.Usage.OutputTokens
    u.CacheReadTokens  += content.Usage.CacheReadInputTokens
    u.CacheWriteTokens += content.Usage.CacheCreationInputTokens
    usage[content.Model] = u
}
```

Each provider backend accumulates a `map[string]TokenUsage` keyed by model
name. The map is returned in `agent.Result.Usage` at session end.

### 3b. Where the data lands

The daemon reports usage independently of task success/failure:

```go
// server/internal/daemon/daemon.go:916-918
if len(result.Usage) > 0 {
    if err := d.client.ReportTaskUsage(ctx, task.ID, result.Usage); err != nil {
        taskLog.Warn("report task usage failed", "error", err)
    }
}
```

On the server, `ReportTaskUsage` (`handler/daemon.go:923-953`) upserts into
`task_usage`:

```sql
-- server/pkg/db/queries/task_usage.sql
INSERT INTO task_usage (task_id, provider, model,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (task_id, provider, model) DO UPDATE SET ...
```

**Schema (`server/migrations/032_task_usage.up.sql`):**
```sql
CREATE TABLE task_usage (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL,
    input_tokens      BIGINT NOT NULL DEFAULT 0,
    output_tokens     BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens  BIGINT NOT NULL DEFAULT 0,
    cache_write_tokens BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (task_id, provider, model)
);
```

**Granularity:** per-task AND per-model (one row per `(task_id, provider, model)`
triplet). Aggregation queries exist for:
- Per-issue: `GetIssueUsageSummary`
- Per-workspace by calendar day: `GetWorkspaceUsageByDay`
- Per-workspace totals: `GetWorkspaceUsageSummary`
- Per-runtime by day: `ListRuntimeUsage`

All are exposed via the server API.

### 3c. Cost calculation — does not exist

**No code computes cost.** Only raw token counts are stored. There is no
`cost_usd` column, no pricing table, and no per-provider rate lookup.

**Natural hook to add cost:** `daemon.go:1152-1166`, where `usageEntries`
is assembled from `result.Usage` just before calling `ReportTaskUsage`. A
pricing map keyed by `(provider, model)` applied here would produce a
`cost_usd` value that can be forwarded in the same HTTP call, stored in a
new `task_usage.cost_usd NUMERIC` column, and aggregated by the existing
`GetWorkspaceUsageSummary`-style queries.

**The closest existing "task complete" hook** is `d.client.ReportTaskUsage`
itself (`daemon.go:916-918`). Extending the `TaskUsageEntry` struct and the
`/api/daemon/tasks/{id}/usage` handler payload is the minimal-touch path.

---

## Risks and Surprises

**No PLAN.md was found in this repository.** The items below note things that
contradict what README.md and CLI_AND_DAEMON.md imply, or common assumptions
a reader might form from those documents.

1. **Daemon uses HTTP polling, not WebSocket, to discover tasks.**
   README.md describes WebSocket as the real-time transport but that is only
   the server→browser channel. The daemon→server channel is plain HTTP poll
   at 3 s intervals. There is no push from the server to the daemon when a
   task is enqueued.

2. **The workdir starts empty — repos are NOT pre-checked-out.**
   CLI_AND_DAEMON.md says "it creates an isolated workspace directory" which
   implies code is present. In reality the directory is empty. The agent must
   call `multica repo checkout <url>` explicitly during execution to get any
   code. An agent that never calls this command runs against an empty directory.

3. **Workspace repos are stored as a JSONB column, not a table.**
   `workspace.repos` is `JSONB NOT NULL DEFAULT '[]'` on the `workspace` row.
   There is no `workspace_repo` table with per-row history or audit trail.
   Repo additions/removals are not individually tracked.

4. **Agent config is re-fetched from the database on every task claim.**
   There is no daemon-side config cache. If an agent's instructions, skills,
   or custom_env are changed while a task is running, the *next* claim will
   pick up the new values. This is intentional but means there is no
   "config snapshot at task start" in the daemon.

5. **The old `runtime_usage` table was dropped (migration 046).**
   Code that referenced it no longer exists. Token usage now lives entirely
   in `task_usage`. Any external tooling or dashboards built against
   `runtime_usage` will fail silently.

6. **stderr from agent CLIs goes to the daemon log only.**
   `cmd.Stderr = newLogWriter(...)` (`claude.go:85`) routes stderr to
   `slog.Debug`. It is NOT forwarded to the server, NOT stored in
   `task_message`, and NOT visible in the frontend. Errors that appear only
   on stderr (e.g. rate-limit warnings printed by the CLI itself) are
   invisible to users.

7. **MCP config is written to the OS temp directory, not the workdir.**
   `os.CreateTemp("", "multica-mcp-*.json")` (`claude.go:536`) puts the file
   in `$TMPDIR`. The file is deleted after the subprocess exits. The agent
   cannot inspect its own MCP config via the filesystem.

8. **No cost computation exists anywhere in the codebase.**
   Despite having a complete token-usage pipeline, there is no pricing table,
   no `cost_usd` field, and no dollar-value anywhere. Adding cost tracking
   requires schema changes (`task_usage.cost_usd NUMERIC`) and a pricing map
   in the daemon.

9. **Session resume has a silent retry that consumes tokens.**
   `daemon.go:1130-1142`: if `--resume` fails (session not found), the daemon
   automatically retries with a fresh session. The first (failed) attempt's
   usage is merged into the second attempt's usage (`mergeUsage`). Users see
   a combined token count with no indication that a retry occurred.

10. **The `bypassPermissions` flag is unconditional for Claude.**
    `buildClaudeArgs` always appends `--permission-mode bypassPermissions`.
    There is no per-agent, per-workspace, or per-skill configuration to
    restrict which tools an agent may call. All tool calls are auto-approved
    for all agents.
