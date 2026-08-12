---
name: multica-creating-agents
description: "Use when creating, inspecting, or debugging a Multica agent definition via the `multica agent` CLI or POST /api/agents. Not for assigning issues to agents that already exist, and not for runtime task prompts."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Creating Multica agents

Contract skill, not a parameter manual: the agent-creation path: what create entry points accept, what the
server validates/rejects, how fields persist, what the daemon reads at claim
time. Every claim is source-traced in
`references/creating-agents-source-map.md`.

## Quick start (read-only)

```bash
multica agent get <agent-id> --output json      # full persisted agent record
multica agent skills list <agent-id> --output json   # current skill bindings
multica agent env get <agent-id> --output json  # plaintext env (agent owner or ws owner/admin; agents denied)
```

**Unbound:** `runtime_id` `NULL` (`runtime_bound: false`) after runtime
deletion (MUL-5559) — keeps everything, editable, no trigger runs
(`agent_runtime_required`) until `agent update <id> --runtime-id <runtime-id>`
binds it. Orthogonal to archived. `agent get` returns `runtime_id`, `model`,
`thinking_level`, `service_tier`, `custom_args`, `has_custom_env`,
`custom_env_key_count`, `skills`; never plaintext `custom_env`.

## Core model

Agent = workspace-scoped row (table `agent`); creation = single
`POST /api/agents` (`multica agent create`). At claim the daemon re-reads the
row — persisted fields, not create-time output, are what runs.

- `description` is a catalog summary; daemon does NOT inject it into the
  prompt; capped at 255 Unicode code points.
- `instructions` is the runtime behavior contract; shipped to the provider at claim.

## CLI / API entry points

```bash
multica agent create --name <name> --runtime-id <runtime-id> \
  --description "<short catalog summary>" \
  --instructions "<runtime behavior contract>" \
  --output json
```

`runAgentCreate` posts to `/api/agents`, adding a key only when its flag was
provided; omitted flags fall through to server defaults. `--max-concurrent-tasks`
validated as 1–50 before sending. Body (`CreateAgentRequest`): `name`,
`description`, `instructions`, `avatar_url`, `runtime_id`, `runtime_config`,
`custom_env`, `custom_args`, `model`, `thinking_level`, `service_tier`,
`visibility`, `max_concurrent_tasks`, `mcp_config`, `skill_ids`.

## Copying an agent

`multica agent copy <source-agent-id>` = headless "Duplicate": `runAgentCopy`
GET /api/agents/<id> and POSTs `CreateAgentRequest` with `skill_ids` —
bindings attach in the SAME atomic create (unlike `agent create`, which binds
nothing).

```bash
multica agent copy <source-agent-id> --name "My Agent (copy)"   # same runtime
multica agent copy <source-agent-id> --runtime-id <target> --model <model>  # cross-runtime fork
```

- Copied by default, overridable: `name` (suffixed `" (copy)"`), `description`,
  `instructions`, avatar, `custom_args`, `max_concurrent_tasks`,
  `permission_mode` + allow-list, workspace skills.
- `max_concurrent_tasks` copied only when source value is within 1–50; explicit
  out-of-range override rejected before any request.
- Runtime fields (`model`, `thinking_level`, `service_tier`) copied ONLY when
  the target runtime is unchanged; a different runtime drops them and REQUIRES
  `--model` (pass `--model ""` for the target default).
- Never copied: `custom_env`, `mcp_config`, `runtime_config`; supply fresh via
  secret-safe flags or `agent env set`. `--no-skills` skips bindings.

## Field contracts

| Field | Persisted as | Validated? | Consumed by |
|---|---|---|---|
| `name` | `agent.name` | required, 400 if empty | listings, runtime payload |
| `description` | `agent.description` | 400 if > 255 code points | catalog only — NOT the prompt |
| `instructions` | `agent.instructions` | none | daemon → provider at claim |
| `avatar_url` | `agent.avatar_url` | none; `avatar_url` → a random `emoji:<glyph>` | catalog UI only |
| `runtime_id` | `agent.runtime_id` (nullable) | required at create (400) | selects runtime; `NULL` = unbound |
| `model` | `agent.model` (nullable) | none beyond runtime support | daemon; empty = runtime default |
| `thinking_level` | `agent.thinking_level` (nullable) | provider-level enum/safe-token gate; unknown → 400; Pi: `off|minimal|low|medium|high|xhigh|max`, daemon checks the model's RPC-discovered subset; ACP runtimes with a `session/new` effort selector (currently `reasonix`) take the safe-token path; no-reasoning runtimes (hermes) reject every non-empty value | daemon; empty = default |
| `service_tier` | `agent.service_tier` (nullable) | Codex-only safe token; other providers reject | daemon → Codex app-server |
| `custom_args` | `agent.custom_args` (JSON array) | JSON shape CLI-side | daemon extra switches; default `[]` |
| `runtime_config` | `agent.runtime_config` (JSON) | JSON shape CLI-side | runtime config; default `{}` |
| `custom_env` | `agent.custom_env` (JSON object) | — | daemon env; see Env & secrets |
| `mcp_config` | `agent.mcp_config` (raw JSON) | JSON object or `null`; create drops literal `null`, update `null` clears | provider MCP; redacted on read |
| `visibility` | `agent.visibility` | — | access; default `private` |
| `max_concurrent_tasks` | `agent.max_concurrent_tasks` | 1–50, else 400 | scheduler cap; default `6` |

Defaults: `max_concurrent_tasks` → `6`; `runtime_config` → `{}`; `custom_env`
→ `{}`; `custom_args` → `[]`; avatar → random `emoji:<glyph>`; `visibility` →
`private` (server-side). Update preserves omitted fields; explicit `0` on
create rejected. `thinking_level`: fixed-vocabulary providers reject unknown
literals; dynamic (Codex/OpenCode) accept safe tokens; Pi's vocabulary is
fixed (`off|minimal|low|medium|high|xhigh|max`) but its model-specific subset
is RPC-discovered; daemon checks the exact model/level pair at execution and
omits incompatible overrides. Claude: `low|medium|high|xhigh|max`; Codex from
catalog.
`service_tier` (currently `priority`, shown as Fast): set with
`--service-tier <catalog-id>`; `--service-tier ""` clears; daemon verifies the
exact model/tier pair; agents without an explicit model fail closed.

`model` is a first-class persisted column; `custom_args` = raw provider CLI args (some
providers reject `--model` there; Pi filters `--thinking` in `custom_args` —
the `thinking_level` field owns that flag).

## Env & secrets

```bash
multica agent create --name <name> --runtime-id <runtime-id> --custom-env-stdin --output json
multica agent create --name <name> --runtime-id <runtime-id> --custom-env-file <0600-json> --output json
```

- Resources never expose plaintext `custom_env` — only `has_custom_env` and
  `custom_env_key_count`.
- Plaintext reads only via `GET /api/agents/{id}/env` (`multica agent env get`),
  gated to the agent's own human owner or workspace owner/admin; **agent
  actors are denied**.
- Writes do NOT go through `agent update` (400: "use PUT /api/agents/{id}/env");
  `PUT /api/agents/{id}/env` (`multica agent env set`) carries the same gate and
  writes an audit row.

### mcp_config

Secret material (entries embed API tokens); same three input channels on
create and update:

```bash
multica agent create --name <name> --runtime-id <runtime-id> --mcp-config-file <0600-json> --output json
multica agent update <agent-id> --mcp-config-stdin --output json
multica agent update <agent-id> --mcp-config 'null'   # clears the config
```

CLI requires a JSON **object** or literal `null`; top-level array/primitive
rejected; empty stdin/file errors. Unlike `custom_env`: settable through
`agent update` (generic `PUT /api/agents/{id}`; omitted → no change, `null` →
clear, object → replace), and serialized on read but redacted (`mcp_config`
`null` + `mcp_config_redacted: true` for non-secret callers; agent actors never
see it).

## Skill binding

Creating does NOT bind skills — separate call after the agent exists:
`add` merges (`POST /api/agents/{id}/skills/add`), `set` replaces all
(`PUT /api/agents/{id}/skills`; `--skill-ids ''` clears).

```bash
multica agent skills add <agent-id> --skill-ids <skill-id> --output json
multica agent skills list <agent-id> --output json
```

At claim: workspace-bound skills FIRST, then platform built-in skills (`LoadAgentSkills`:
compile-time embedded, `SKILL.md` + siblings) — capability belongs in a bound
skill, not `instructions`.

## Side effects

Read-only: `agent get`, `agent skills list`, `agent env get`. State-changing
(explicit instruction required): `multica agent create`, `multica
agent copy` (source untouched), `agent skills add`/`set` (`set` destructive),
`agent env set` (overwrites + audit row).

## Common wrong assumptions

- "`description` is the prompt" — only `instructions` reaches the runtime.
- "Create binds skills" — bind explicitly afterward.
- "`agent update` can rotate env" — it 400s on `custom_env`.
- "`mcp_config` behaves like `custom_env` on update" — `mcp_config` IS settable
  via `agent update` (`--mcp-config null` clears).
- "`agent get` shows env values" — only `has_custom_env` +
  `custom_env_key_count`.
- "Invalid `thinking_level`/`model` combo is caught at create" — model-specific
  gaps fail at run time.
- "`set` and `add` are interchangeable" — `set` silently removes capabilities.

## References

`references/creating-agents-source-map.md` maps every contract above to
`file:line`, runtime effect, and a safe read-only verification command.
