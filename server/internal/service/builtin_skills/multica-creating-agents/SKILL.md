---
name: multica-creating-agents
description: "Use when creating, inspecting, or debugging a Multica agent through the `multica agent` CLI or `POST /api/agents` — what each field is, its persisted shape, whether it is metadata-only or consumed by the daemon at claim time, which inputs are validated/rejected, how custom_env secrets are gated, and how skill binding behaves. Not for assigning issues to existing agents or for runtime task prompts."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Creating Multica agents

This is the contract for Multica's agent-creation path: what the create entry
points accept, what the server validates and rejects, how each field is
persisted, and which fields the daemon actually reads at claim time. It is
not a parameter manual — it states source-traced facts, and every claim is
backed by `file:line` in `references/creating-agents-source-map.md`.

## Quick start (read-only inspection)

These commands read state and have no side effects:

```bash
multica agent get <agent-id> --output json      # full persisted agent record
multica agent skills list <agent-id> --output json   # current skill bindings
multica agent env get <agent-id> --output json  # plaintext env (owner/admin only, agents denied)
```

`agent get` returns the persisted agent including `runtime_id`, `model`,
`thinking_level`, `custom_args`, `has_custom_env`, `custom_env_key_count`, and
`skills`. It never returns plaintext `custom_env`.

## Core model

An agent is a workspace-scoped row (table `agent`). Creation is a single
`POST /api/agents` (`multica agent create`). At task claim time the daemon
re-reads the agent row and assembles the runtime payload — so the persisted
fields, not the create-time output, are what the agent runs on.

Two distinct text fields, often confused:

- `description` is a catalog summary. It is stored and shown in listings; the
  daemon does NOT inject it into the agent's runtime prompt. Treat it as
  human-facing metadata only. Capped at 255 Unicode code points.
- `instructions` is the runtime behavior contract. The daemon reads it at
  claim time and ships it to the provider as the agent's durable instructions.
  Persona, responsibilities, boundaries, output and escalation rules go here,
  not in `description`.

## CLI / API entry points

Minimum create call (`--name` and `--runtime-id` are both required):

```bash
multica agent create --name <name> --runtime-id <runtime-id> \
  --description "<short catalog summary>" \
  --instructions "<runtime behavior contract>" \
  --output json
```

`runAgentCreate` builds a JSON body and posts it to `/api/agents`. It only
adds a key when its flag was provided — `description`/`instructions` on a
non-empty value, the rest (`runtime-config`, `custom-args`, `model`,
`thinking-level`, `visibility`, …) on the flag being `Changed` — so omitted
flags fall through to server defaults rather than sending empty strings.

The HTTP body (`CreateAgentRequest`) accepts: `name`, `description`,
`instructions`, `runtime_id`, `runtime_config`, `custom_env`, `custom_args`,
`model`, `thinking_level`, `visibility`, `max_concurrent_tasks`, `mcp_config`.

## Field contracts

| Field | Persisted as | Validated? | Consumed by |
|---|---|---|---|
| `name` | `agent.name` | required, 400 if empty | listings, runtime payload |
| `description` | `agent.description` | 400 if > 255 code points | catalog/listing only — NOT the runtime prompt |
| `instructions` | `agent.instructions` | none | daemon → provider at claim time |
| `runtime_id` | `agent.runtime_id` | required (400) + must resolve to a runtime in this workspace | selects runtime/provider |
| `model` | `agent.model` (nullable) | none beyond runtime support | daemon reads; empty = runtime default |
| `thinking_level` | `agent.thinking_level` (nullable) | provider-level enum; unknown literal → 400 | daemon; empty = runtime default |
| `custom_args` | `agent.custom_args` (JSON array) | JSON shape checked CLI-side; server stores as-is | daemon (extra CLI switches); defaults to `[]` |
| `runtime_config` | `agent.runtime_config` (JSON) | JSON shape checked CLI-side; server stores as-is | runtime-specific config; defaults to `{}` |
| `custom_env` | `agent.custom_env` (JSON object) | — | daemon (process env); see Env & secrets |
| `mcp_config` | `agent.mcp_config` (raw JSON) | CLI checks it is a JSON object or `null`; server stores as-is. At create, literal `null` is dropped (no-op); at update, `null` clears the column | daemon → provider (MCP servers) — **runtime-consumed**; redacted on read |
| `visibility` | `agent.visibility` | — | access control; defaults to `private`; gates who can read/route a private agent (e.g. a private squad leader) — NOT the runtime prompt |
| `max_concurrent_tasks` | `agent.max_concurrent_tasks` | — | scheduler task cap; defaults to `6` |

Defaults when omitted: `runtime_config` → `{}`, `custom_env` → `{}`,
`custom_args` → `[]`, `visibility` → `private`, `max_concurrent_tasks` → `6`
(all materialized server-side before the insert). `custom_args`/`runtime_config`
are typed `[]string`/`any` and marshaled as-is — the JSON-shape rejection
happens in the CLI, not the create handler.

`thinking_level` is validated only at the provider level: an unrecognized
literal returns 400, but a value that is valid for the provider yet
unsupported for the chosen model is NOT rejected here — that gap surfaces as a
daemon-side task error at execution time.

Set it from the CLI with `--thinking-level` on `agent create` and `agent
update`, mirroring `--model`: the flag is a thin pass-through to the top-level
`thinking_level` field, and on update an empty string (`--thinking-level ""`)
clears it back to the runtime default. The CLI deliberately does not enumerate
the valid levels — they are runtime/model-specific (Claude
`low|medium|high|xhigh|max`, Codex `none|minimal|low|medium|high|xhigh`, and
others), so it forwards whatever you pass and lets the server's provider
catalog accept or reject it. A runtime whose provider has no thinking concept
rejects any non-empty value with a 400.

### model vs custom_args

`model` is a first-class persisted column the daemon reads directly.
`custom_args` are raw provider CLI args. The CLI help notes that some providers
(codex app-server, openclaw) reject `--model` inside `custom_args` — but that is
documented CLI guidance, not a server-enforced invariant; nothing in the create
handler inspects `custom_args` for a model flag.

## Env & secrets

`custom_env` is secret material. The CLI offers three input channels; two keep
secrets out of shell history and the process list:

```bash
multica agent create --name <name> --runtime-id <runtime-id> --custom-env-stdin --output json
multica agent create --name <name> --runtime-id <runtime-id> --custom-env-file <0600-json> --output json
```

`--custom-env-stdin` reads the JSON object from stdin; `--custom-env-file`
reads it from a file (suggested mode 0600). The third channel,
`--custom-env <json>`, puts the value on the command line where shell history
and `ps` can see it — avoid it for real secrets.

Read-side facts (these are the wrong assumptions to avoid):

- Agent resources never expose plaintext `custom_env`. `agent
  list/get/create/update` and WS events return only `has_custom_env` (bool) and
  `custom_env_key_count` (int).
- Reading plaintext values requires the dedicated `GET /api/agents/{id}/env`
  endpoint (`multica agent env get`). It is gated to workspace **owner/admin**
  members, and **agent actors are denied** regardless of the backing member's
  role — a running agent cannot read another agent's secrets.
- Writing values after creation does NOT go through `agent update`. The generic
  update handler rejects any `custom_env` field with a 400 ("use PUT
  /api/agents/{id}/env"). Plaintext env writes are handled by
  `PUT /api/agents/{id}/env` (`multica agent env set`), which is owner/admin-only
  and writes an audit row.

### mcp_config

`mcp_config` is the agent's MCP server configuration (a JSON object such as
`{"mcpServers": {…}}`). It is also secret material — MCP entries routinely embed
API tokens — and offers the same three input channels as `custom_env`, on BOTH
`agent create` and `agent update`:

```bash
multica agent create --name <name> --runtime-id <runtime-id> --mcp-config-file <0600-json> --output json
multica agent update <agent-id> --mcp-config-stdin --output json
multica agent update <agent-id> --mcp-config 'null'   # clears the config
```

`--mcp-config-stdin` / `--mcp-config-file` keep the value out of shell history
and `ps`; the inline `--mcp-config <json>` does not. The CLI requires a JSON
**object** or the literal `null`; a top-level array or primitive is rejected
client-side, and empty stdin/file input errors rather than silently clearing.

Two ways `mcp_config` differs from `custom_env`:

- **It IS settable through `agent update`.** Unlike `custom_env`, `mcp_config`
  has no dedicated audited endpoint — the generic `PUT /api/agents/{id}` accepts
  it. Tri-state per the raw request body: field omitted → no change; `null` →
  clear; object → replace.
- **It is serialized on read, but redacted.** `agent get`/`list` return
  `mcp_config` only to callers allowed to view agent secrets; otherwise the
  field is `null` and `mcp_config_redacted` is `true`. Agent actors never see
  it, and a workspace may force redaction for everyone.

## Skill binding

Creating an agent does NOT bind any workspace skill — binding is a separate
call after the agent exists. Two distinct verbs:

- `add` is additive — it merges the given ids with existing bindings
  (`POST /api/agents/{id}/skills/add`).
- `set` is replace-all — it overwrites the entire binding list with exactly
  the given ids (`PUT /api/agents/{id}/skills`); `--skill-ids ''` clears all.

```bash
multica agent skills add <agent-id> --skill-ids <skill-id> --output json
multica agent skills list <agent-id> --output json
```

At claim time the daemon assembles the agent's skills as workspace-bound skills
FIRST, then appends the platform built-in skills. `LoadAgentSkills` loads each
bound skill's content plus its supporting files; built-in skills are embedded
at compile time and loaded from `SKILL.md` + sibling files. Both reach the
provider as skill content — which is why capability belongs in a bound skill,
not pasted into `instructions`.

## Side effects needing approval

Read-only (safe): `agent get`, `agent skills list`, `agent env get`.

State-changing (require an explicit instruction — do not run speculatively):

- `multica agent create` — inserts a new agent row.
- `multica agent skills add` / `set` — mutate bindings (`set` is destructive:
  it drops bindings not in the new list).
- `multica agent env set` — overwrites the full `custom_env` map and writes an
  audit row.

## Common wrong assumptions

- "`description` is the prompt." It is not — only `instructions` reaches the
  runtime. A rich description with empty instructions yields a named shell with
  no operating contract.
- "Create binds the agent's skills." It does not; bind explicitly afterward.
- "`agent update` can rotate env." It cannot — it 400s on `custom_env`; use the
  env endpoint.
- "`mcp_config` behaves like `custom_env` on update." It does not — `mcp_config`
  IS settable via `agent update` (`--mcp-config`), with `--mcp-config null` to
  clear; only `custom_env` is gated behind the dedicated env endpoint.
- "`agent get` shows env values." It shows only `has_custom_env` and
  `custom_env_key_count`.
- "An invalid `thinking_level`/`model` combo is caught at create." Only an
  unknown provider-level literal is — model-specific gaps fail at run time.
- "`set` and `add` are interchangeable for skills." `set` replaces all
  bindings; using it when you meant `add` silently removes capabilities.

## References

`references/creating-agents-source-map.md` maps every contract above to its
`file:line` on the current tree, the runtime effect, and a safe read-only
verification command.
