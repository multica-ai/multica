---
name: multica-deterministic-tools
description: "Use when deciding whether a Multica skill's behavior should become a deterministic Go tool in the dettools plane, or when adding/changing a tool under server/pkg/dettools. Covers the skill-vs-deterministic-code decision framework (advisory guidance stays a skill; correctness-sensitive, mechanical, gateable behavior becomes a Go tool), the Result envelope and stable error codes, how to register a tool and set its allowlist, the read-only/path-scoped security rules, daemon-side MCP injection, per-agent tool profiles, and the rule that converting a skill means updating its SKILL.md and source-map in the same change."
user-invocable: true
allowed-tools: Bash(go *), Bash(make *)
---

# Multica Deterministic Tools

Guide for converting skill behavior into the **deterministic tool plane**
(`dettools`) — and, more importantly, for deciding when a piece of behavior
should stay a skill versus become typed Go code.

## The two planes

Multica runs agents on two planes (see `docs/plans/deterministic-tools-plan.md`):

| Plane | What it is | Failure mode |
|---|---|---|
| **Skill** (advisory) | Markdown loaded into the agent's context. *Describes* what to do. The model reads it and may follow, paraphrase, or ignore it. | A wrong or stale skill is a *suggestion the model acted on* — silent, recoverable. |
| **Deterministic tool** (`dettools`) | Typed Go handler exposed over MCP. *Does* the thing and returns a verifiable `Result` with a stable contract and an audit log. | A wrong tool is a *bug with a stack trace* — caught by tests, fails closed. |

A skill is prose the model interprets. A tool is code that executes. That single
difference drives every decision below.

## Decision framework: skill vs deterministic code

The litmus test, from the plan's goal statement:

> Skills remain available for advisory guidance (task framing, conventions).
> **Anything correctness-sensitive moves into deterministic tool handlers.**

Apply it field by field. Ask three questions:

1. **Is a wrong answer a correctness bug, or just suboptimal advice?**
   - Correctness bug ("did the tests actually pass?", "what is the real current
     branch?", "is this changed file under a forbidden path?") → **tool**. The
     model must not be allowed to *guess* the answer.
   - Suboptimal advice ("how to frame a PR description", "which status to move an
     issue to", "naming conventions") → **skill**. Judgment is what the model is
     good at.

2. **Is the operation mechanical and schema-stable, or contextual and fluid?**
   - Mechanical: same input → same output, no judgment (parse git state, run a
     command, normalize an exit code, write an artifact). → **tool**.
   - Contextual: depends on intent, team norms, the issue's nuance, prose. →
     **skill**.

3. **Does it need to be enforced, sandboxed, or audited?**
   - Needs a hard gate, path scoping, a timeout, or a per-invocation audit record
     → **tool** (the daemon enforces these; a skill cannot).
   - Pure guidance with no enforcement surface → **skill**.

### Keep it a skill when

- It frames a task, sequences CLI calls, or teaches a convention
  (`multica-working-on-issues`, `multica-mentioning`, `multica-squads`). The
  underlying `multica` CLI is *already* deterministic; the skill only teaches
  intent and ordering.
- The right answer depends on judgment, team norms, or the text of an issue.
- It changes often or is workspace-specific (a tool ships in the binary; a skill
  is editable Markdown).
- A wrong answer is recoverable and non-critical.

### Convert to a deterministic Go tool when

- It reports a **fact about the repo** the model would otherwise hallucinate —
  branch, changed files, lockfiles, package managers (`repo_facts`).
- It **enforces a policy** rather than suggesting one — branch-name rules,
  forbidden paths, required files. A skill *asks* the model to check; a tool
  *returns* `POLICY_FAILURE` with the violation list (`policy_check`).
- It **runs real commands and normalizes the outcome** — build/restore probes,
  smoke suites (`build_probe`, `test_gate`, `dotnet_test_gate`). "Did it pass?"
  must be measured, never asserted by the model.
- It is a **deterministic transform** the model would otherwise approximate — a
  stable machine-readable diff summary (`diff_summarize`).
- It **emits a structured artifact** other steps or the UI consume
  (`artifact_emit`).
- The same operation recurs across tasks and benefits from typed I/O, a stable
  error contract, and an audit trail.

### The hybrid pattern (most common outcome)

Converting a skill is rarely wholesale. Usually you **extract the
correctness-critical core into a tool and keep a thin skill that orchestrates
it**:

> The deterministic part: "is the branch valid, are forbidden paths touched, did
> the smoke suite pass" → `policy_check` + `test_gate` tools. For C# test
> gates, prefer `dotnet_test_gate` so missing SDK/PATH and test failures are
> structured outcomes.
> The advisory part that stays a skill: "before opening a PR, run the policy
> check; if it returns `POLICY_FAILURE`, fix the violation before pushing."

When you find yourself writing a skill sentence like *"make sure X is actually
true"* — that sentence is a tool. The skill should say *"call the X tool and act
on its `Result`,"* not re-describe the check in prose the model might fudge.

## How to add or convert a tool

A tool lives in `server/pkg/dettools/tool_<name>.go`. Use an existing tool as the
template — `tool_policy_check.go` is the cleanest reference.

1. **Define the input struct + JSON Schema.** All fields optional where possible;
   an empty input should behave sensibly. Set `"additionalProperties": false` in
   the schema and decode with `strictUnmarshal` (rejects unknown fields →
   `INVALID_INPUT`). This is the "parse, don't cast" boundary for tool input.

2. **Write the `<name>Tool() Tool` constructor** returning `Name`, a
   `Description` the agent sees, the `InputSchema`, and the `Handler`.

3. **Write the handler.** It must be **read-only** and must not write outside
   `env.WorkDir` / `env.ArtifactDir`. Return a `Result` via `OK(summary, data)`
   or `Errf(code, ...)`. Check dependencies first (`gitAvailable()`,
   `isGitRepo`) and return `MISSING_DEPENDENCY` rather than crashing.

4. **Register it** in `allTools()` in `registry.go`. This is the only wiring
   step — the registry filters to the allowlist automatically.

5. **Decide the default allowlist.** If the tool should be on by default, add its
   name to `DefaultDetToolsAllowed` in `server/internal/daemon/config.go`. Only
   non-destructive, read-only tools belong in the default set. Anything with side
   effects stays off-by-default and is opt-in via `MULTICA_DETTOOLS_ALLOWED`.

6. **Test it fail-closed** in `server/pkg/dettools/*_test.go`: valid input, each
   error code it can return, malformed/missing fields (must fail closed, not
   pass), and path-escape rejection if it touches paths. A new tool with no
   malformed-input test does not meet the contract.

## The Result contract (`contract.go`)

Every tool returns the same envelope. Do not invent per-tool shapes.

```go
Result{
  Status:      "ok" | "error",
  Summary:     "human-readable one-liner",
  MachineData: map[string]any{...}, // structured facts the agent branches on
  Artifacts:   []Artifact{...},     // optional, path relative to WorkDir
  Retryable:   bool,
  ErrorCode:   "INVALID_INPUT" | ...,
}
```

Use the helpers: `OK(summary, data)` for success, `Errf(code, format, args...)`
for failure. **Error codes are a frozen contract** — agents and the daemon branch
on them, so never repurpose a value once shipped:

- `INVALID_INPUT` — bad/unknown fields. Not retryable.
- `MISSING_DEPENDENCY` — required tool (git, a toolchain) absent. Not retryable.
- `POLICY_FAILURE` — a deterministic gate failed. Not retryable (same input,
  same failure).
- `TIMEOUT` / `INTERNAL_ERROR` — transient; `Errf` marks these **retryable**.

The retryable/not split is mechanical: deterministic failures retried yield the
same result, so they are not retryable; only `TIMEOUT` and `INTERNAL_ERROR` are.

## Security constraints (non-negotiable for v1)

The daemon enforces these via `ToolEnv`; a handler that breaks them is the bug:

- **Read-only.** No `git clean`, `rm`, DB writes, or source mutation. Destructive
  tools are out of scope for v1 and would require an interactive approval gate.
- **Path-scoped.** Operate within `env.WorkDir`; reject absolute paths and `..`
  traversal (see `artifacts.go`, which rejects names escaping the artifact dir).
- **Network denied** unless `env.AllowNetwork` (from
  `MULTICA_DETTOOLS_ALLOW_NETWORK`, default off).
- **Timed.** Respect `env.Timeout`; the daemon watchdogs back this up.
- **Allowlisted.** Only `MULTICA_DETTOOLS_ALLOWED` tools are registered.
- **Audited.** Every invocation is logged (tool, outcome, duration, input size,
  artifacts). Do not add a side channel that bypasses the audit log.

## How tools reach the agent

You do not call tools from a skill directly — the daemon injects them as an MCP
server. `server/internal/daemon/dettools_inject.go` merges a `multica-tools`
server entry (`command` = the multica binary, `args` = `mcp-tools serve`) into
the agent's `mcp_config`. The injection is **additive**: user-defined MCP servers
are preserved. Providers in `dettoolsExecOptionsProviders` (claude, codex,
opencode, hermes, kimi, kiro) get it through `ExecOptions`; OpenClaw through
`execenv`; Pi through a project-local `.pi/mcp.json` adapter file.

**Per-agent tool profiles:** an agent's `runtime_config` may carry
`deterministic_tools.{allowed_tools, denied_tools}`. An agent can only **narrow**
the daemon allowlist, never widen it. The whole plane is opt-in via
`MULTICA_DETTOOLS_ENABLED` (default off) and fails open — if injection fails, the
agent launches without the tool plane and the daemon logs a warning.

## When you convert a skill, update the skill

This is a hard CLAUDE.md rule, not a nicety. If you move correctness-sensitive
behavior out of a skill into a tool:

1. Edit the skill's `SKILL.md` so it stops re-describing the check in prose and
   instead points the agent at the tool's `Result`.
2. Update the skill's `references/*-source-map.md` to cite the new tool's source.
3. Do both **in the same change** as the Go code. The built-in skills are
   source-traced contracts shipped to agents; if the code moves and the skill
   doesn't, the skill silently teaches stale behavior.

Run `make check` (or at minimum `cd server && go test ./pkg/dettools/...` and the
builtin-skill conformance tests) before pushing.

More source-backed detail: `references/deterministic-tools-source-map.md`.
