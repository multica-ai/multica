# Agent tool brief — how an agent learns every tool it has (FIR-2312)

This is the canonical pattern for surfacing tools to an agent in its prompt.
**Read this before adding a new tool family, connection type, or permission that
an agent should be able to discover and use.** New capabilities reach the agent
by flowing through this one path — do not hand-write a new brief section per
tool.

## The problem it solves

The agent brief's `## Available Commands` block used to be a hand-written, static
list of `multica` CLI commands only. It never listed **MCP tools** or
**connections** (external API / MCP servers a workspace admin wires in), and it
was not tied to permissions — so an agent literally did not know which
connections it had, and the list could not shrink when a permission was removed.

## The pattern (one path, three layers)

```
toolaccess.ListEffectiveTools  →  AgentTaskResponse.effective_tools  →  cerebroToolsBrief
   (server: resolve+filter)         (claim payload: ship)               (brief: render)
```

1. **Resolve (server, at claim time).** The claim handler
   `ClaimTaskByRuntime` calls `cerebroEffectiveToolsForBrief`
   ([server/internal/handler/daemon_effective_tools_cerebro.go](../../server/internal/handler/daemon_effective_tools_cerebro.go)),
   which reuses `h.runtimeToolAccess.ListEffectiveTools` — the **same** resolver
   the admin "effective access" UI uses. It folds the full tool-policy chain
   (Workspace › Runtime › Agent › Group › User), keeps only tools whose
   `ExposureEffective.Effective` is true, and maps each to an
   `AgentTaskToolEntry{Family, Name, Description, Verdict}`. Family is derived
   from the tool source: a tool backed by a connection's MCP server → `Connections`
   (name prefixed with the server), `source == "mcp"` → `MCP tools`, otherwise
   `Platform tools`.

2. **Ship (claim payload).** The entries ride to the daemon in the claim
   response as `effective_tools`
   ([AgentTaskResponse](../../server/internal/handler/agent.go), mirrored by the
   daemon [Task](../../server/internal/daemon/types.go) struct). This is the same
   transport every shipped-at-claim field already uses (e.g.
   `disallowed_mcp_tools`).

3. **Render (brief).** The daemon copies the entries into
   `TaskContextForEnv.EffectiveTools`, and
   `cerebroToolsBrief`
   ([server/internal/daemon/execenv/cerebro_tools_brief.go](../../server/internal/daemon/execenv/cerebro_tools_brief.go))
   renders them into `## Available Commands` as a grouped, deterministic section
   with a short primer on what a connection is. Empty input → no section.

Because the **same** tool-policy resolution drives both this list and live call
enforcement, the two can never disagree: remove a permission and the tool drops
out of the resolved set and disappears from the brief automatically.

## Why it works across all (local) runtime providers

`cerebroToolsBrief` is wired into `buildMetaSkillContent`, the single shared
brief builder for every local provider (Claude → `CLAUDE.md`, Codex/Cursor/Kiro/…
→ `AGENTS.md`, Gemini → `GEMINI.md`). One render path covers them all; see
[runtime-prompt-architecture.md](./runtime-prompt-architecture.md).

**Cloud (Firtal Gateway) runtimes assemble their prompt in a separate, upstream
code path that is not in this repo.** The shipped `effective_tools` field is the
runtime-agnostic seam: the gateway prompt builder must read the same field and
render it the same way to reach parity. Until it does, the dynamic tools section
is a local-runtime feature. Track this as the cross-runtime verification step.

## Adding a new tool / connection / permission

You almost certainly do **not** touch the brief. Make the tool appear in the
runtime tool inventory and be governed by the tool-policy chain (the normal way a
tool or connection is added). It will then:

- be resolved by `ListEffectiveTools`,
- shipped in `effective_tools` when exposed to the agent,
- rendered in the brief grouped under the right family.

Only extend `cerebroEffectiveToolsForBrief` if you introduce a genuinely new
**family grouping**, and only extend `cerebroToolsBrief` if you change how a
family renders. Keep both deterministic (the brief must be byte-stable across
identical inputs).

## Tests

- Renderer: `server/internal/daemon/execenv/cerebro_tools_brief_test.go`
- Resolver mapping/filtering: `server/internal/handler/daemon_effective_tools_cerebro_test.go`
