# Deterministic tools source map

The deterministic tool plane (`dettools`) is pure Go compiled into the `multica`
binary and spawned per task over MCP stdio. Design rationale and phase status:
`docs/plans/deterministic-tools-plan.md`.

## Package layout (`server/pkg/dettools/`)

- `contract.go` defines the `Result` envelope, the `OK` / `Errf` helpers, and the
  frozen error codes (`INVALID_INPUT`, `MISSING_DEPENDENCY`, `POLICY_FAILURE`,
  `TIMEOUT`, `INTERNAL_ERROR`). `Errf` marks only `TIMEOUT` and `INTERNAL_ERROR`
  retryable.
- `registry.go` defines `Tool`, `Handler`, `ToolEnv`, and `allTools()` — the one
  place every tool is registered. `NewRegistry(allowed)` filters to the allowlist;
  an empty allowlist exposes everything (direct CLI use only — the daemon always
  passes an explicit allowlist).
- `server.go` boots the MCP stdio server (`multica mcp-tools serve`).
- `options.go` builds `ToolEnv` (WorkDir, AllowNetwork, Timeout, ArtifactDir,
  Logger).
- `exec_util.go` / `git.go` hold the command and git helpers
  (`runShell`, `runCommand`, `gitAvailable`, `isGitRepo`, `currentBranch`,
  `changedFiles`).
- `artifacts.go` resolves artifact paths and rejects names that escape the
  artifact dir (absolute paths, `..` traversal).
- `strictUnmarshal` (in `tool_repo_facts.go`) decodes tool input with
  `DisallowUnknownFields()` — the "parse, don't cast" boundary; unknown fields →
  `INVALID_INPUT`.

## Tool catalog (`tool_*.go`)

- `tool_repo_facts.go` — branch, modified files, lockfiles, package managers.
- `tool_policy_check.go` — branch-prefix / forbidden-path / required-file gate;
  returns `POLICY_FAILURE` with a `violations` list. Cleanest template for a new
  tool.
- `tool_build_probe.go` — detect toolchains, run a non-destructive build/restore.
- `tool_test_gate.go` — run configured smoke suites, normalize outcomes.
- `tool_dotnet_test_gate.go` — run `dotnet test` with structured arguments,
  returning `MISSING_DEPENDENCY` when `dotnet` is unavailable and
  `POLICY_FAILURE` when tests/coverage fail.
- `tool_diff_summarize.go` — stable machine-readable changed-file summary.
- `tool_artifact_emit.go` — write JSON/Markdown under the task artifact dir.

All seven are read-only / non-destructive. Tests: `dettools_test.go`,
`tools_phase2_test.go`.

## CLI entry point

- `server/cmd/multica/cmd_mcp_tools.go` registers `multica mcp-tools serve`, the
  subcommand the daemon points an agent's `mcp_config` `command` at.

## Daemon injection

- `server/internal/daemon/dettools_inject.go` —
  `buildEffectiveMcpConfig(agentCfg, selfBin, workDir, cfg, allowed)` merges the
  `multica-tools` server (`dettoolsServerName`) into the agent's `mcp_config`,
  additively. `dettoolsExecOptionsProviders` lists the providers wired through
  `ExecOptions` (claude, codex, opencode, hermes, kimi, kiro). Per-agent profile
  (`agentDetToolsProfile`: `allowed_tools` / `denied_tools`) is read from
  `runtime_config.deterministic_tools` and can only narrow the daemon allowlist.
  Tests: `dettools_inject_test.go`.
- `server/internal/daemon/dettools_pi.go` — writes/merges `.pi/mcp.json` for the
  pi-mcp-adapter path (opt-in `MULTICA_DETTOOLS_PI_ADAPTER`, fail-open). Tests:
  `dettools_pi_test.go`.

## Configuration (`server/internal/daemon/config.go`)

Env-var driven, parsed in `LoadConfig` onto `DetToolsConfig`:

- `MULTICA_DETTOOLS_ENABLED` (default false) — master switch.
- `MULTICA_DETTOOLS_ALLOWED` — allowlist; defaults to `DefaultDetToolsAllowed`
  (the full non-destructive catalog).
- `MULTICA_DETTOOLS_DENIED` — daemon-wide denylist.
- `MULTICA_DETTOOLS_TIMEOUT` (`DefaultDetToolsTimeout`, 90s).
- `MULTICA_DETTOOLS_ALLOW_NETWORK` (default false).
- `MULTICA_DETTOOLS_ARTIFACT_DIR` (`.multica/artifacts`).
- Pi knobs: `MULTICA_DETTOOLS_PI_ADAPTER`, `MULTICA_DETTOOLS_PI_CONFIG_PATH`
  (`.pi/mcp.json`), `MULTICA_DETTOOLS_PI_INSTALL_CMD`.

## Skill ↔ tool coupling rule

CLAUDE.md: when code a built-in skill documents changes, update that skill's
`SKILL.md` **and** `references/*-source-map.md` in the same change. Moving a
correctness-sensitive check out of a skill into a `dettools` handler is exactly
this case.
