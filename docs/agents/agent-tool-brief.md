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

   **api-type connection endpoints are the one exception (FIR-2388).** They are
   server-side-dispatched HTTP endpoints, not runtime-inventory tools, so
   `ListEffectiveTools` does not see them. `cerebroEffectiveToolsForBrief` appends
   them separately through the injected `CerebroAPIConnectionBriefResolver` — the
   **same** `runtime.APIConnectionResolver.ListForAgent` the cloud gateway and the
   local `multica mcp serve` handler use to build and gate these tools. So a listed
   api-connection tool in the brief is, by construction, one the agent can actually
   call. They render under `Connections` with the verbatim tool name (e.g.
   `infisical_admin__get_api_v3_secrets_raw`) and their Allow/Ask verdict. The
   resolver is injected (not imported) because it lives in the `runtime` package,
   which imports `handler` — the reverse import would be a cycle.

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
   with a short primer on what a connection is. Empty input → no section. The
   primer also states the **api-connection argument shape** up front — path
   parameters at the top level, query parameters inside a `query` object, request
   body inside `body` (FIR-2441) — so an agent never has to call-and-read-the-error
   to discover the `query` object. That wording is the shared
   `runtime.APIConnectionArgHint` constant (see the cloud note below), so the
   local brief and the cloud system prompt teach the identical rule.

Because the **same** tool-policy resolution drives both this list and live call
enforcement, the two can never disagree: remove a permission and the tool drops
out of the resolved set and disappears from the brief automatically.

## Why it works across all (local) runtime providers

`cerebroToolsBrief` is wired into `buildMetaSkillContent`, the single shared
brief builder for every local provider (Claude → `CLAUDE.md`, Codex/Cursor/Kiro/…
→ `AGENTS.md`, Gemini → `GEMINI.md`). One render path covers them all; see
[runtime-prompt-architecture.md](./runtime-prompt-architecture.md).

**Cloud (Firtal Gateway) runtimes assemble their system prompt in the
`runtime.FirtalGatewayExecutor` tool loops
([firtal_gateway_executor.go](../../server/internal/cerebro/runtime/firtal_gateway_executor.go)),
a separate path from the local brief builder.** Note there are **two** cloud tool
loops and the compat one is the live default: `runGatewayCompatRegistryToolLoop`
(builds its prompt in `withRegistryToolUsageHint`) is the primary path per the
`firtal-gateway-tool-loop-fallback` patch; `runAnthropicToolLoop` is the
Anthropic-native fallback. Any prompt-parity change must land in **both**. Two
levels of parity apply:

- **Connection-guidance parity (done, FIR-2441 fix-list #5).** When an
  api-connection tool is actually offered (detected via
  `registryHasAPIConnectionTool` in the native loop / `toolsHaveAPIConnectionTool`
  in the compat loop, not a name pattern), both cloud loops append the full
  `runtime.ConnectionGuidance` constant — the same text the local brief renders,
  which embeds `runtime.APIConnectionArgHint`. So a cloud agent and a local agent
  learn the identical "what a connection is" + `query`-object rule; the two first
  prompts cannot drift on it. Before this the live compat loop shipped only a bare
  tool-name list, so a cloud agent discovered the `query` shape by calling and
  failing.
- **Full tool-list parity (still tracked).** The flat cloud "You have the
  following tools available: …" line is built from the offered tool names, not yet
  from the shipped `effective_tools` entries with their families/verdicts. Making
  the cloud prompt render `effective_tools` the same way the local brief does is
  the remaining cross-runtime verification step (a same-agent/same-permissions
  diff test of the two first prompts).

## Adding a new tool / connection / permission

You almost certainly do **not** touch the brief. Make the tool appear in the
runtime tool inventory and be governed by the tool-policy chain (the normal way a
tool or connection is added). It will then:

- be resolved by `ListEffectiveTools`,
- shipped in `effective_tools` when exposed to the agent,
- rendered in the brief grouped under the right family.

Only extend `cerebroEffectiveToolsForBrief` if you introduce a genuinely new
**family grouping** or a tool family that does not flow through the runtime tool
inventory (as api-type connection endpoints do — see the FIR-2388 note above), and
only extend `cerebroToolsBrief` if you change how a family renders. Keep both
deterministic (the brief must be byte-stable across identical inputs).

## Tests

- Renderer: `server/internal/daemon/execenv/cerebro_tools_brief_test.go`
- Resolver mapping/filtering: `server/internal/handler/daemon_effective_tools_cerebro_test.go`
