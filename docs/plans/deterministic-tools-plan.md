# Multica Deterministic Tool Plane — Implementation Plan

Status: proposed
Branch: `claude/multica-deterministic-tools-5qrf89`

## 1. Goal

Add a **deterministic tool plane** to Multica so agents can invoke typed,
auditable Go-backed tools through MCP, instead of relying only on
prompt-defined skills for correctness-sensitive behavior. The agent runtime
model (Claude Code, Codex, Pi, etc.) is unchanged — this is additive.

Two planes:

| Plane | Role | Execution |
|---|---|---|
| Agent plane | Planning, coding, reasoning, freeform task execution | Existing daemon → CLI backend flow |
| Deterministic tool plane | Repo inspection, policy checks, build/test probes, artifact generation | Go MCP server compiled into the daemon binary, spawned locally per task |

Skills remain available for advisory guidance (task framing, conventions).
Anything correctness-sensitive moves into deterministic tool handlers.

## 2. Key architectural decision: the tool plane is **fully Go**

The MCP host **and** the tool implementations are written in Go and compiled
into the existing `multica` binary (`server/cmd/multica`). No Python, no Node,
no separate artifact to distribute.

Rationale:

- **Zero new runtime dependency.** The daemon ships as a standalone Go binary
  (GoReleaser → Homebrew, see CLAUDE.md "CLI Release"). Go tools compile into
  it; Python/Node would require a runtime that may be absent on the host. This
  closes the dependency-risk concern outright rather than managing it.
- **Zero distribution wrinkle.** The MCP server is a new subcommand on the
  binary that already exists on the machine. The daemon points the agent's
  `mcp_config` `command` at its own binary path. Nothing extra to ship.
- **Reuses existing daemon machinery.** Subprocess exec, the idle/tool
  watchdogs (`MULTICA_AGENT_IDLE_WATCHDOG`, `MULTICA_AGENT_TOOL_WATCHDOG`),
  per-task isolated workdirs, and the local trust boundary already exist in the
  daemon. The tools (`repo_facts`, `policy_check`, `build_probe`, `test_gate`,
  `diff_summarize`, `artifact_emit`) are systems-level operations — shelling out
  to git/build tools and inspecting the filesystem — which is idiomatic Go.
- **No real loss.** The "schema reuse with `packages/core`" argument for TS is
  illusory: `packages/core` zod schemas describe API responses consumed by the
  frontend. Deterministic tool I/O contracts are a new thing consumed by the
  *agent over MCP* — there is no existing schema to reuse, only the pattern. Go
  defines equivalent JSON-schema contracts natively.

MCP Go SDK: evaluate `github.com/modelcontextprotocol/go-sdk` (official) vs.
`github.com/mark3labs/mcp-go`. Pin via go.mod; prefer the official SDK if its
stdio server API is stable at implementation time.

## 3. Current state (verified against the codebase)

- **Backends** are created in `server/pkg/agent/agent.go` `New()` for: claude,
  codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor, kimi, kiro,
  antigravity.
- **MCP-capable providers** are exactly: `claude, codex, hermes, kimi, kiro,
  opencode, openclaw` (`packages/core/agents/mcp-support.ts`
  `MCP_SUPPORTED_PROVIDERS`). The MCP config tab is hidden for all others.
- **`mcp_config` is a per-agent, user-authored field** (`agent.mcp_config`
  JSONB, `migration 046_agent_mcp_config`). It flows verbatim:
  `task.Agent.McpConfig` → `ExecOptions.McpConfig` → backend
  (`server/internal/daemon/daemon.go` ~L2682 for `execenv.Prepare/Reuse`, and
  ~L2871–2934 for `ExecOptions`). **There is no daemon-side generation or merge
  of MCP servers today.**
- **Two MCP materialization paths exist:**
  1. `ExecOptions.McpConfig` — claude (`claude.go:42-50`, `--mcp-config <file>`),
     codex (`codex.go:191-206`, `[mcp_servers.*]` in `config.toml`), opencode
     (`OPENCODE_CONFIG_CONTENT` env), hermes/kimi/kiro (ACP `mcpServers` array
     with capability negotiation).
  2. **OpenClaw** materializes `mcp.servers` via the per-task wrapper preparer in
     `server/internal/daemon/execenv/` — **not** through `ExecOptions`.
- **Pi has no MCP path.** `pi.go` `buildPiArgs()` never reads `McpConfig`; it
  uses `--append-system-prompt`. No `pi-mcp-adapter` exists anywhere in the repo.
- **Daemon config is env-var driven** (`server/internal/daemon/config.go`),
  `MULTICA_*`. There is no YAML/JSON daemon config file.
- **The `multica` binary already hosts the daemon** as a subcommand
  (`server/cmd/multica/cmd_daemon.go`). Adding an MCP-serve subcommand is the
  natural extension. Concurrency cap default is 20
  (`MULTICA_DAEMON_MAX_CONCURRENT_TASKS`).
- **Skills** are embedded builtin (`server/internal/service/builtin_skills/`)
  plus workspace skills, written into the per-task workdir.

## 4. Core design

### 4.1 Go MCP server as a binary subcommand

Add `multica mcp-tools serve` (new file `server/cmd/multica/cmd_mcp_tools.go`).
It runs an MCP server over **stdio**, registering the Go tool handlers in-process.
A new package `server/pkg/dettools/` holds the host + tool registry + handlers.

```
server/pkg/dettools/
  server.go        # MCP stdio server bootstrap, tool registration
  registry.go      # tool catalog + allowlist filtering
  contract.go      # shared input/output envelope + error codes
  tools/
    repo_facts.go
    policy_check.go
    build_probe.go
    test_gate.go
    diff_summarize.go
    artifact_emit.go
  *_test.go
```

The agent CLI (claude/codex) spawns this subcommand per the injected
`mcp_config`. Because the command is the daemon's own binary, it is always
present and version-matched.

### 4.2 Daemon-side MCP merge (the new core component)

This is the primary net-new piece. Today the daemon passes the user's
`agent.mcp_config` through untouched. We add a single merge helper that produces
an **effective** MCP config = user-defined servers + the daemon-managed
deterministic server.

`server/internal/daemon/dettools_inject.go`:

```go
// buildEffectiveMcpConfig merges the daemon-managed deterministic tool server
// into the agent's user-authored mcp_config. Additive: never drops or overrides
// a user-defined server. Returns the original config unchanged when the tool
// plane is disabled or the provider can't reach it.
func buildEffectiveMcpConfig(
    agentCfg json.RawMessage,
    provider string,
    selfBinPath string,
    d *Daemon,
) (json.RawMessage, error)
```

- Injected server entry (Claude-style, the canonical input shape the backends
  already accept):

  ```json
  {
    "mcpServers": {
      "multica-tools": {
        "command": "<path to multica binary>",
        "args": ["mcp-tools", "serve"],
        "env": { "MULTICA_DETTOOLS_ALLOWED": "repo_facts,policy_check" }
      }
    }
  }
  ```

- **Wire it at both read sites** in `daemon.go`. `agentMcpConfig` is read once
  for `execenv.Prepare/Reuse` (~L2682) and once for `ExecOptions` (~L2871).
  Call `buildEffectiveMcpConfig` once, feed the result into **both** so the
  `ExecOptions` path (claude/codex/opencode/hermes/kimi/kiro) and the
  `execenv`/OpenClaw materialization path stay consistent. (Phase 1/2 scope is
  claude+codex via `ExecOptions`; OpenClaw is covered because the same merged
  config feeds `execenv`.)
- `selfBinPath`: the daemon already resolves its own executable path for
  auto-update; reuse that. Fall back to `os.Executable()`.

### 4.3 Tool contract

Every tool validates input against a versioned JSON schema and returns a fixed
envelope.

- **Input:** JSON object, versioned schema, validated before handler runs.
- **Output envelope:**

  ```json
  {
    "status": "ok",
    "summary": "Repository policy check passed",
    "machine_data": { "branch": "feature/x", "dirty_files": 0, "tests_run": 42 },
    "artifacts": [ { "type": "json", "path": ".multica/artifacts/policy-result.json" } ],
    "retryable": false
  }
  ```

- **Error codes** (stable): `INVALID_INPUT`, `MISSING_DEPENDENCY`,
  `POLICY_FAILURE`, `TIMEOUT`, `INTERNAL_ERROR`.
- **Audit:** the server logs request payload, normalized output, duration, exit
  code, and artifact metadata per invocation (structured `slog`, consistent
  with the daemon).

### 4.4 Initial tool catalog (all read-only / non-destructive)

- `repo_facts` — branch, modified files, lockfiles, package managers, changed
  projects.
- `policy_check` — branch naming, PR rules, required files, forbidden paths.
- `build_probe` — detect toolchains (Go, Node, .NET, Rust, Java) and run a
  non-destructive build/restore probe.
- `test_gate` — run configured smoke suites, normalize outcomes.
- `diff_summarize` — stable machine-readable summary of changed files.
- `artifact_emit` — write JSON/Markdown artifacts under the task artifact dir.

No destructive operations (`git clean`, `rm`, DB writes) in v1. If added later,
they require an explicit approval gate (§7).

## 5. Configuration (env vars, not YAML)

Consistent with `daemon/config.go`. No new config file.

| Env var | Default | Meaning |
|---|---|---|
| `MULTICA_DETTOOLS_ENABLED` | `false` | Master switch for the tool plane |
| `MULTICA_DETTOOLS_ALLOWED` | `repo_facts,policy_check,build_probe,test_gate` | Allowlisted tool names |
| `MULTICA_DETTOOLS_TIMEOUT` | `90s` | Default per-tool timeout |
| `MULTICA_DETTOOLS_ALLOW_NETWORK` | `false` | Whether tools may touch the network |
| `MULTICA_DETTOOLS_ARTIFACT_DIR` | `.multica/artifacts` | Artifact output dir (relative to task workdir) |

Parsed in `LoadConfig` and surfaced on `Config`. Per-agent overrides come from a
new optional `tool_profile` field (§9, Phase 4) so different agent roles see
different tools.

## 6. Provider integration

| Provider | MCP path | Daemon action |
|---|---|---|
| Claude Code | Native (`--mcp-config`) | Merge deterministic server into effective config; launch normally |
| Codex | Native (`config.toml [mcp_servers.*]`) | Same merge; launch normally |
| OpenCode / Hermes / Kimi / Kiro | Native | Covered by the same merged config (later phase) |
| OpenClaw | `execenv` materialization | Covered because merged config feeds `execenv.Prepare` |
| Pi | None natively | Adapter path, per-task (§6.1, Phase 3) |

Injection is **additive**: user-defined MCP servers are preserved alongside
`multica-tools`.

### 6.1 Pi (Phase 3, validated against pi-mcp-adapter)

Pi has no native MCP and reaches the plane through `pi-mcp-adapter`
(github.com/nicobailon/pi-mcp-adapter). Validated facts from that repo:

- **Install:** `pi install npm:pi-mcp-adapter` (requires a Pi restart). The
  adapter exposes a single `mcp` proxy tool with lazy, on-demand discovery.
- **Config schema:** `{ "settings": {...}, "mcpServers": { "<name>": {command,
  args, env, cwd, url, headers, ...} } }` — Claude-style `mcpServers`, exactly
  what our merge emits.
- **Discovery (precedence):** `~/.config/mcp/mcp.json` → `<Pi agent
  dir>/mcp.json` (`~/.pi/agent`) → `.mcp.json` → **`.pi/mcp.json`** (project
  override, highest precedence), each read relative to the agent cwd.

This makes the **global-file race a non-issue without any env override**: the
daemon writes the project-local **`.pi/mcp.json`** into the per-task work dir
(which is the agent's cwd), so each task gets its own config by construction.

Implemented flow (`daemon/dettools_pi.go`, opt-in via
`MULTICA_DETTOOLS_PI_ADAPTER`, fail-open):

1. Detect provider == `pi` (and the plane is enabled).
2. Compute the per-agent effective tool allowlist (§9 policy).
3. Merge `multica-tools` into the work dir's `.pi/mcp.json`, preserving any
   existing user servers and top-level `settings`.
4. On `local_directory` tasks (the user's own repo), restore the original file
   after the run so no daemon state is left behind; ephemeral work dirs are GC'd.
5. Launch Pi; the installed adapter discovers `.pi/mcp.json` and reaches
   `multica-tools` through its `mcp` proxy tool.

Note `pi install` needs a restart to take effect, so a task-time install can't
help the current run — operators install the adapter once;
`MULTICA_DETTOOLS_PI_INSTALL_CMD` is an optional best-effort hook, off by default.

## 7. Security model

Deterministic tools are code execution, not prompt context. Enforced by the
daemon / MCP host:

- **Path scoping** — tools operate within the task workdir; reject paths outside
  it.
- **Network** — denied unless `MULTICA_DETTOOLS_ALLOW_NETWORK=true`.
- **Timeouts** — per-tool, default `MULTICA_DETTOOLS_TIMEOUT`; also covered by
  existing daemon watchdogs.
- **Allowlist** — only `MULTICA_DETTOOLS_ALLOWED` tools are registered/served.
- **Env allowlist** — handlers see a filtered environment.
- **Audit log** — per-invocation request/output/duration/exit code/artifacts.
- **No destructive ops in v1**; future destructive tools require interactive
  approval.

## 8. Capability reporting

Distinguish how each provider reaches the tool plane (don't hide differences):

- `native_mcp = true` — claude, codex (and the other MCP-capable backends).
- `adapter_mcp = true` — pi, only when `pi-mcp-adapter` is installed and healthy.
- `tool_plane_supported = true` — any provider that can reach `multica-tools`
  via either path.

Backend home: `agent_runtime.metadata` (JSONB) already exists for registered
runtimes. Frontend signal builds on `providerSupportsMcpConfig()` in
`packages/core/agents/mcp-support.ts`. Surface in the agent/runtime UI.

## 9. Delivery phases

| Phase | Scope | Outcome |
|---|---|---|
| 1 ✅ | Go MCP server subcommand + 2 read-only tools (`repo_facts`, `policy_check`); merge helper; inject for **claude** only | End-to-end tool plane on Claude Code — **implemented** (`server/pkg/dettools/`, `multica mcp-tools serve`, `daemon/dettools_inject.go`) |
| 2 ✅ | Add codex injection; full tool catalog (`build_probe`, `test_gate`, `diff_summarize`, `artifact_emit`); artifact writing + audit logging | **Implemented** — codex added to `dettoolsExecOptionsProviders` (same Claude-style shape renders into its `config.toml`); six-tool catalog; `artifact_emit` writes path-scoped artifacts; per-invocation audit log records tool, outcome, duration, input size, and artifact paths |
| 3 ✅ | Extend to opencode/hermes/kimi/kiro (ExecOptions) + openclaw (execenv); capability reporting; Pi via `pi-mcp-adapter` | **Implemented & validated** — all six native-MCP providers receive the tool server; `mcpSupportKind`/`toolPlaneSupported` classify native/adapter/none (pi → adapter). **Pi adapter validated against the real `pi-mcp-adapter`** (github.com/nicobailon/pi-mcp-adapter): config schema is `{settings, mcpServers}` (Claude-style, matches our output), discovered via the project-local `.pi/mcp.json` (highest-precedence, read relative to the agent cwd). `daemon/dettools_pi.go` writes/merges `.pi/mcp.json` into the task work dir — per-task by construction, no global-file race — preserving any existing user servers/`settings`, and restores the original on `local_directory` tasks. Opt-in (`MULTICA_DETTOOLS_PI_ADAPTER`), fail-open; knobs `MULTICA_DETTOOLS_PI_CONFIG_PATH` and `MULTICA_DETTOOLS_PI_INSTALL_CMD`. |
| 4 ✅ | Policy controls + per-agent tool profiles + capability reporting | **Implemented** — per-agent `deterministic_tools.{allowed_tools,denied_tools}` read from `runtime_config` (plumbed daemon-side, no migration); daemon-wide `MULTICA_DETTOOLS_DENIED` denylist; agents can only narrow the daemon allowlist. Richer invocation-log UI deferred (needs an invocation data model — daemon currently audit-logs only). |

## 10. Failure handling

- claude/codex: if merge/injection fails, fail fast or launch without the tool
  plane per workspace policy (configurable; default: launch without, log warn).
- Pi: adapter missing/misconfigured → tool plane unavailable for Pi + remediation
  message; never pretend MCP works.
- MCP host unhealthy: surface in task diagnostics and capability status.

## 11. Testing

- `server/pkg/dettools/*_test.go` — Go unit tests per tool: valid input, each
  error code, malformed/missing fields (fail closed), path-escape rejection,
  timeout.
- `server/internal/daemon/dettools_inject_test.go` — merge is additive
  (preserves user servers), no-op when disabled, correct entry for claude vs
  codex, feeds both read sites.
- Capability reporting test for native vs adapter vs unsupported.
- Per CLAUDE.md: update affected builtin skills' `SKILL.md` +
  `references/*-source-map.md` if CLI/behavior they document changes (e.g.
  `multica-runtimes-and-repos`).
- `make check` (typecheck, unit, Go, E2E) before push.

## 12. Open questions / external dependencies

- **`pi-mcp-adapter` interface** — install command, config path, proxy-tool
  discovery model. Not in repo; must be validated against the real package
  before Phase 3. All Phase 3 detail is provisional until then.
- **Go MCP SDK choice** — official `modelcontextprotocol/go-sdk` vs
  `mark3labs/mcp-go`; confirm stdio server API stability and pin a version.
- **Failure-mode default** for native providers when injection fails (fail-fast
  vs degrade) — confirm with product before Phase 2.
