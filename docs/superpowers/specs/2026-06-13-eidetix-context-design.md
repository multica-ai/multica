# Eidetix Shared-Context Integration — Design

**Status:** Approved design, pre-implementation
**Date:** 2026-06-13
**Author:** Naman (with Claude)
**Scope:** v0 — marketing workflows only, project-scoped

## Problem

Multica agents start every task cold. When an agent is assigned an issue it
rebuilds context from the issue body and comments alone; nothing an agent
learns (decisions, brand facts, campaign outcomes) persists for the next agent
or the next issue. There is no shared memory or knowledge graph across agent
runs. The result is duplicated discovery work and inconsistent outputs across
a team of agents working the same body of work.

A partner (Eidetix) ships a hosted MCP service that is exactly this: a cited,
queryable knowledge graph with both a read path (recall the team's recorded
knowledge) and a write path (ingest new observation traces). This project
wires Eidetix into Multica's agent runtime so that agents working
marketing issues share one brain — read relevant prior knowledge before they
act, and write back what they learn.

## Eidetix interface (from partner docs)

- **Transport:** hosted SaaS, MCP over SSE / streamable-http at
  `https://eidetix.nodeops.xyz/mcp/sse`.
- **Auth:** `Authorization: Bearer <token>` header.
- **The token selects the graph.** Each token maps to one graph/domain. The
  partner issued two tokens for this rollout: a **Marketing** token and a
  **Support** token. There is no separate namespace parameter — presenting a
  token *is* selecting its graph.
- **Tools (8):**
  - Read: `recall` (one-shot cited recall, returns document + graph sections),
    `search`, `get_graph`, `get_graph_expanded`, `get_content`,
    `resolve_entities`.
  - Write: `get_schema` (must be called before ingest), `ingest_traces`.
- The partner's own guidance is "zero-code, identical to OpenClaw/Multica":
  from Eidetix's side, integration is adding one MCP server entry with the URL
  and Bearer header.

**Token values are secrets and are NOT recorded in this spec or in git.** They
live only in the encrypted DB column and in the operator's hands.

## Decisions (from brainstorming)

1. **Read + write loop** — agents both query Eidetix for prior knowledge and
   persist what they learn. The full shared brain, not read-only.
2. **Binding unit = project.** Each Multica project maps to one Eidetix graph
   (via one token). Every issue carries `project_id`, so the graph is always
   deterministically resolvable from the issue. Issues with no project get no
   Eidetix (expected).
3. **Approach A — server-side MCP merge + a conditionally-shipped loop skill.**
   The integration lives entirely in the task-claim handler plus a new config
   table and CLI command. The daemon and provider backends are unchanged.
4. **Config surface: CLI only** for v0. No UI panel, no REST-only path.
5. **No workspace-level kill-switch.** The per-project `enabled` flag is the
   rollout control.

## Why Approach A

Multica's daemon already treats the task-claim response's `mcp_config`
(Claude-style `{"mcpServers": {...}}`) as the authoritative managed MCP set and
forwards it uniformly to every provider:

- OpenClaw: synthesized per-task config
  (`server/internal/daemon/execenv/openclaw_config.go`), which already accepts
  HTTP/SSE entries via `url` + `transport`.
- All other providers: `agent.ExecOptions.McpConfig`
  (`server/internal/daemon/daemon.go:2989`).

Both the `Prepare` and `Reuse` paths read the same `task.Agent.McpConfig`
(`daemon.go:2724` / `:2738`), and the value is recomputed every claim — so
merging server-side is a single chokepoint that (a) reaches all providers, and
(b) stays fresh on reused workdirs with no extra work.

Crucially, **no daemon or CLI release is required.** Every daemon already in
the field — including the live CreateOS cloud runtime — picks up a merged
`eidetix` server the next time it claims a task. This is the decisive advantage
over a daemon-side merge (which would strand the live deployment until upgraded).

The decrypted token rides the claim response, which is the exact channel that
already carries tokened agent `mcp_config` today. No new trust boundary.

## Architecture

```
project (eidetix_project_config: enabled, endpoint_url, token_encrypted, graph_label)
  │
issue.project_id ──► task-claim handler (server)
  │   if project has Eidetix enabled:
  │     • decrypt token
  │     • merge {eidetix: {url, transport, headers:{Authorization: Bearer …}}}
  │       into resp.Agent.McpConfig.mcpServers
  │     • append the multica-eidetix loop skill to resp.Agent.Skills
  ▼
daemon (UNCHANGED) ──► ExecOptions.McpConfig / OpenClaw synth ──► provider backend ──► agent run
  │
  agent: recall/search (read) ──► do the work ──► get_schema → ingest_traces (write)
```

## Components

### C1. Data model — `eidetix_project_config`

New migration under `server/migrations/` (next sequential number).

| column | type | notes |
|---|---|---|
| `project_id` | uuid | PK, FK → `projects(id)` ON DELETE CASCADE |
| `enabled` | boolean | NOT NULL DEFAULT true — soft disable without losing the token |
| `endpoint_url` | text | NOT NULL, default `https://eidetix.nodeops.xyz/mcp/sse` |
| `token_encrypted` | bytea | NOT NULL — encrypted with the same helper Lark installations use |
| `graph_label` | text | NULL — human label ("Marketing"/"Support"); never the token |
| `created_at` | timestamptz | NOT NULL DEFAULT now() |
| `updated_at` | timestamptz | NOT NULL DEFAULT now() |

sqlc queries (`server/pkg/db/queries/`):
- `GetEidetixConfigForProject(project_id)` → row or no-rows.
- `UpsertEidetixProjectConfig(project_id, enabled, endpoint_url, token_encrypted, graph_label)`.
- `DeleteEidetixProjectConfig(project_id)`.

Regenerate with `make sqlc`.

### C2. Token encryption

Reuse the existing encryption helper used for `lark_installation`
encrypted secrets (`server/internal/handler/lark.go` and its service layer).
Encrypt on write, decrypt only server-side at claim time. The token is never
returned by any API or CLI read path.

### C3. CLI command — `multica project eidetix`

New subcommand group under the existing project CLI
(`server/cmd/multica/cmd_*.go`, alongside agent/skill command patterns):

- `multica project eidetix set <project> --token <t> [--endpoint <url>] [--label <l>]`
  — upsert; reads token from `--token`, `--token-stdin`, or `--token-file`
  (mirror the secure-input modes `multica agent` uses for `mcp_config` so the
  token never lands in shell history).
- `multica project eidetix show <project>` — prints `enabled`, `endpoint_url`,
  `graph_label`, `configured: true/false`. **Never prints the token.**
- `multica project eidetix clear <project>` — delete the config.
- `multica project eidetix disable/enable <project>` — flip `enabled`.

Backed by an owner/admin-gated REST handler
(`server/internal/handler/`, new `eidetix.go`) following the `agent_env.go`
audited-write pattern. `project_id` resolved through the existing project
loader; writes use the resolved `project.ID` (per the UUID-parsing convention).

### C4. Claim-handler merge (`server/internal/handler/daemon.go`)

After the issue's project is resolved (~`:1189`, where `project_id` is known),
in a **fail-open** block:

1. `GetEidetixConfigForProject(project.ID)`. No row or `enabled == false` →
   skip entirely.
2. Decrypt `token_encrypted`.
3. Build the server entry:
   ```json
   {
     "url": "<endpoint_url>",
     "transport": "streamable-http",
     "headers": { "Authorization": "Bearer <token>" }
   }
   ```
4. Merge into `resp.Agent.McpConfig`:
   - Parse existing `mcpServers` (or start `{}` if the agent had no config).
   - If a server literally named `eidetix` already exists (user-defined), do
     **not** clobber it; log a warning and skip the merge.
   - Otherwise add `eidetix`, re-marshal, assign back to `resp.Agent.McpConfig`.
5. Append the loop skill: `resp.Agent.Skills = append(resp.Agent.Skills,
   h.TaskService.EidetixLoopSkill()...)`.

The merge only mutates the **claim-response copy**, never the stored agent
record — so the agent-detail/MCP-Tab UI is unaffected and shows only what the
admin actually configured.

### C5. The loop skill — `multica-eidetix`

A new embedded skill that is **NOT** part of `BuiltinSkills()` (which ships to
every agent unconditionally). Instead it's exposed via a dedicated accessor
`TaskService.EidetixLoopSkill()` and appended only in the Eidetix-enabled
branch of the claim handler.

Layout under `server/internal/service/` (embedded like `builtin_skills/`):
- `SKILL.md` — teaches the loop:
  - **Before acting:** `recall` or `search` the issue topic; `resolve_entities`
    for people/tool/service names; `get_graph`/`get_content` to expand and read
    sources. Treat hits as shared team memory; cite what informs the work.
  - **After meaningful work or a decision:** call `get_schema` first, then
    `ingest_traces` to persist durable facts (decisions made, brand/voice
    facts, campaign outcomes). Guidance on what's worth ingesting (durable, not
    transient) and to avoid duplicating existing entries.
- `references/eidetix-tools-source-map.md` — the source-traced contract
  (tool names → behavior), per the repo rule that built-in skills are
  source-traced. Because the source of truth here is the partner's MCP server,
  this map points at the partner doc + the eight tool names rather than repo
  files.

The skill must satisfy `TestBuiltinSkillsConformToTemplate` invariants.

## Provider / transport constraint

Eidetix is a remote HTTP/SSE MCP server. A provider backend's translation of
`McpConfig` must support a `url`-based entry for the `eidetix` server to load.

- **Known-good:** Claude Code (remote MCP via `url`/`type`), OpenClaw
  (`transport: streamable-http` — the partner explicitly demos this).
- **Must verify per-backend during implementation:** Codex, Hermes, Gemini,
  kiro, kimi. A backend that only speaks stdio MCP will reject or ignore the
  entry.

**Operational constraint:** marketing agents must run on a verified provider.
The implementation plan includes a verification task that exercises the
`eidetix` entry against each provider backend Multica's marketing agents will
realistically use, and documents the supported set. We do not assume a backend
ignores an unknown HTTP entry gracefully — that is checked, not presumed.

## Error handling

Principle: **Eidetix must never block or fail a task.**

- **Claim-time** (no config / decrypt error / malformed agent `mcp_config` /
  marshal error): log and proceed with the task unchanged (no `eidetix` server,
  no skill).
- **Run-time endpoint down / bad token:** the agent's MCP tool calls fail; the
  agent continues without memory. Surfaced in agent logs, not as a Multica task
  failure.
- **Issue has no `project_id`:** no Eidetix (expected; per the binding decision).

## Testing

- **Go (handler):**
  - Enabled project → response `mcp_config` contains `eidetix` with correct
    url/transport/Authorization, and the loop skill is appended.
  - Disabled / absent config → neither the server nor the skill is added.
  - Decrypt failure → task still claims successfully (fail-open).
  - Pre-existing agent `mcp_config` is preserved and `eidetix` is added.
  - User-defined server named `eidetix` is not clobbered.
- **Go (skill):** the `multica-eidetix` skill conforms to the
  built-in-skill template invariants.
- **Go (crypto):** token encrypt/decrypt round-trip.
- **Provider verification:** an integration check (or documented manual
  procedure) confirming the `eidetix` entry loads on each supported backend.
- **No frontend tests** (CLI-only surface, no UI).

## Rollout

1. Run the migration; regenerate sqlc.
2. `multica project eidetix set <marketing-project> --token-stdin --label Marketing`.
3. Assign a marketing issue to an agent running a verified provider.
4. Confirm in agent logs that `eidetix` tools are available and that the agent
   calls `recall`/`search` before acting and `ingest_traces` after.
5. Per-project `enabled=false` is the off switch.

## Documentation / contract upkeep

Per repo rules, the same PR that adds the `multica project eidetix` command
must update any built-in skill that documents project CLI commands (e.g.
`multica-projects-and-resources`) and its source map, so agents are not taught
stale command surface.

## Out of scope (v0)

- Hard read enforcement (server pre-calling `recall` and injecting cited
  context into the brief).
- Hard write enforcement (daemon post-run hook auto-calling `ingest_traces`
  with the session transcript). Deferred until we measure whether the
  skill-driven write path is actually exercised by agents.
- UI config panel (web/desktop).
- Per-team/per-agent graph binding.
- Workspace-level kill-switch.

## Future path to Approach B

If Eidetix becomes one of several project-scoped context providers, the
resolver generalizes from "get the eidetix config" to "list this project's MCP
servers," and the merge can move from server to daemon behind a new
task-response field. The `eidetix_project_config` table and the claim-handler
resolver are designed so that is a refactor, not a rewrite.
