---
name: multica-creating-agents
description: "Use when creating, inspecting, or debugging a Multica agent definition via the `multica agent` CLI or POST /api/agents. Not for assigning issues to agents that already exist, and not for runtime task prompts."
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
multica agent env get <agent-id> --output json  # plaintext env (agent owner or ws owner/admin; agents denied)
```

An agent can also be **unbound**: `runtime_id` is `NULL` (served as `""` with
`runtime_bound: false`) after its runtime was deleted, which unbinds instead of
deleting its agents (MUL-5559). An unbound agent keeps everything it owns and
stays editable, but no trigger path will run it — they all refuse with
`agent_runtime_required` — until `agent update <id> --runtime-id <runtime-id>` binds
it again. Unbound is orthogonal to archived.

`agent get` returns the persisted agent including `runtime_id`, `model`,
`thinking_level`, `service_tier`, `custom_args`, `has_custom_env`,
`custom_env_key_count`, and `skills`. It never returns plaintext `custom_env`.

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
`thinking-level`, `service-tier`, `visibility`, …) on the flag being `Changed`
— so omitted flags fall through to server defaults rather than sending empty
strings. `--max-concurrent-tasks` is validated as 1–50 before the request is
sent.

The HTTP body (`CreateAgentRequest`) accepts: `name`, `description`,
`instructions`, `avatar_url`, `runtime_id`, `runtime_config`, `custom_env`,
`custom_args`, `model`, `thinking_level`, `service_tier`, `visibility`,
`permission_mode`, `invocation_targets`, `max_concurrent_tasks`, `mcp_config`,
`skill_ids`.

## Invocation permission

`permission_mode` + `invocation_targets` are the authorization source for
assigning, mentioning, chatting with, or otherwise starting an agent. The
legacy `visibility` field is only a derived compatibility projection:

- `private`: only the human owner may invoke.
- `public_to` + `workspace`: any member or workspace-internal agent/system
  principal may invoke.
- `public_to` + `member`: only that exact human user may invoke.
- `public_to` + `agent`: only that exact, active, same-workspace agent actor may
  invoke. This edge is direct and non-transitive: A → B and B → C does not grant
  A → C.
- `team` is stored but inert until team membership has an authority source.

The owner can set an exact agent edge from create, update, or copy:

```bash
multica agent create --name <name> --runtime-id <runtime-id> \
  --public-to-agent <source-agent-id> --output json
multica agent update <target-agent-id> \
  --permission-mode public_to --public-to-agent <source-agent-id> --output json
multica agent copy <source-agent-id> \
  --public-to-agent <new-invoker-agent-id> --output json
```

`--public-to-agent` and `--public-to-member` are repeatable and may be combined.
An agent target must be a non-archived user agent in the same workspace;
malformed, missing, archived, system, and cross-workspace ids are rejected.
Duplicate ids are canonicalized idempotently. Writes are human-owner-only:
task-scoped agent actors cannot configure these grants even when their backing
user owns the target. Every successful explicit permission write records a
minimal `agent_invocation_permission_updated` activity row containing only the
mode and target type/id pairs.

Invocation permission grants task creation only. They never grant another agent
access to the target's env, MCP config, human chat transcripts, run history, or
unrelated workspace data. Agent metadata remains discoverable for routing, with
secret fields redacted.

## Copying an agent

`multica agent copy <source-agent-id>` forks an existing agent's portable
configuration into a brand-new agent, leaving the source untouched. It is the
CLI/headless equivalent of the web "Duplicate" action. No dedicated server API
is involved: `runAgentCopy` reads the source with `GET /api/agents/<id>`, then
POSTs a `CreateAgentRequest` — passing the source's skill ids in `skill_ids` so
the bindings attach in the SAME create transaction (unlike `agent create`, which
binds nothing). The mutation is therefore a single atomic create.

```bash
multica agent copy <source-agent-id> --name "My Agent (copy)"   # same runtime
multica agent copy <source-agent-id> --runtime-id <target> --model <model>  # cross-runtime fork
```

- Copied by default, each overridable with the matching flag: `name` (suffixed
  `" (copy)"`), `description`, `instructions`, avatar, `custom_args`,
  `max_concurrent_tasks`, invocation permission (`permission_mode` +
  allow-list), and assigned workspace skills.
- Agent invocation targets are copied verbatim when the copy remains in the
  same workspace. The server revalidates every target on create, so a stale,
  archived, or foreign target fails closed instead of being silently widened.
- A copied `max_concurrent_tasks` is included only when the source value is
  within 1–50. Historical out-of-range values are omitted so the new agent
  receives the server default (`6`); an explicit out-of-range
  `--max-concurrent-tasks` override is rejected before any API request.
- Runtime-specific fields (`model`, `thinking_level`, `service_tier`) are copied
  ONLY when the target runtime is unchanged. `--runtime-id` selecting a
  different runtime drops them and REQUIRES `--model` (pass `--model ""` to
  accept the target runtime default), mirroring the web Duplicate clearing model
  on a runtime switch.
- Never copied: `custom_env`, `mcp_config`, `runtime_config` (secret /
  machine-local; redacted or masked on read anyway). Supply fresh values with
  the same secret-safe flags as `agent create` (`--custom-env*`, `--mcp-config*`,
  `--runtime-config`), or with `agent env set` after the copy exists.
- `--no-skills` skips copying the source's skill bindings.

## Field contracts

| Field | Persisted as | Validated? | Consumed by |
|---|---|---|---|
| `name` | `agent.name` | required, 400 if empty | listings, runtime payload |
| `description` | `agent.description` | 400 if > 255 code points | catalog/listing only — NOT the runtime prompt |
| `instructions` | `agent.instructions` | none | daemon → provider at claim time |
| `avatar_url` | `agent.avatar_url` | none; an explicit non-empty value is preserved, while omitted/empty creates a random `emoji:<glyph>` avatar | catalog/listing UI only — NOT the runtime prompt |
| `runtime_id` | `agent.runtime_id` (nullable) | required at create (400) + must resolve to a runtime in this workspace | selects runtime/provider; `NULL` means unbound — see below |
| `model` | `agent.model` (nullable) | none beyond runtime support | daemon reads; empty = runtime default |
| `thinking_level` | `agent.thinking_level` (nullable) | provider-level enum/safe-token gate; unknown literal → 400. Pi accepts only `off|minimal|low|medium|high|xhigh|max`, then the daemon checks the selected model's RPC-discovered subset. ACP runtimes that advertise an effort selector in `session/new` (currently `reasonix` and `hermes`) take the safe-token path and are checked against the discovered catalog by the daemon; that catalog covers only the model the discovery session was on, so other models show no picker until per-model probing exists. `hermes` covers two binaries — jcode advertises and applies an effort, Hermes Agent advertises none and gets no picker — so the answer there comes from the runtime's discovered catalog, not the provider name. Because that catalog is only written once a client requests a model list, a `hermes` runtime that has never been discovered is refused with a distinct "has not reported a model catalog yet" 400 rather than being assumed capable; `reasonix`, whose provider name does determine the binary, is allowed in that state. A runtime with no reasoning control at all (e.g. `copilot`, which executes outside ACP) rejects EVERY non-empty value and says so — that 400 is a capability answer, not a bad token | daemon; empty = runtime default |
| `service_tier` | `agent.service_tier` (nullable) | Codex-only safe token; other providers reject; exact model/tier pair checked by daemon | daemon → Codex app-server; empty = local Codex config |
| `custom_args` | `agent.custom_args` (JSON array) | JSON shape checked CLI-side; server stores as-is | daemon (extra CLI switches); defaults to `[]` |
| `runtime_config` | `agent.runtime_config` (JSON) | JSON shape checked CLI-side; server stores as-is | runtime-specific config; defaults to `{}` |
| `custom_env` | `agent.custom_env` (JSON object) | — | daemon (process env); see Env & secrets |
| `mcp_config` | `agent.mcp_config` (raw JSON) | CLI checks it is a JSON object or `null`; server stores as-is. At create, literal `null` is dropped (no-op); at update, `null` clears the column | daemon → provider (provider-specific MCP handling); redacted on read |
| `visibility` | `agent.visibility` | derived from invocation permission | legacy compatibility projection; `workspace` only for `public_to` + workspace target, otherwise `private` |
| `permission_mode` + `invocation_targets` | `agent.permission_mode` + `agent_invocation_target` rows | owner-only; agent targets must be active same-workspace agents | authoritative invoke/assign/@mention/chat gate; agent targets match only the immediate actor and are non-transitive |
| `max_concurrent_tasks` | `agent.max_concurrent_tasks` | integer from 1 through 50; out-of-range values return 400 | scheduler task cap; defaults to `6` |

Defaults when omitted or explicitly `null`: `max_concurrent_tasks` → `6`.
Other defaults when omitted: `runtime_config` → `{}`, `custom_env` → `{}`,
`custom_args` → `[]`, `avatar_url` → a random `emoji:<glyph>`, `visibility` →
`private`
(all materialized server-side before the insert). `custom_args`/`runtime_config`
are typed `[]string`/`any` and marshaled as-is — the JSON-shape rejection
happens in the CLI, not the create handler.

The 1–50 concurrency range applies consistently to create and update. On
create, an omitted field defaults to 6 while an explicitly supplied 0 is
rejected; on update, omission preserves the current value. The CLI performs the
same range check before sending create or update requests.

`thinking_level` is validated only at the provider level: fixed-vocabulary
providers reject an unrecognized literal, while dynamic-vocabulary providers
such as Codex/OpenCode accept a syntactically safe token. Pi's provider-level
vocabulary is fixed (`off|minimal|low|medium|high|xhigh|max`), but its exact
supported subset is model-specific and discovered from the local Pi RPC model
catalog. A value unsupported for the chosen model is NOT rejected here — the
daemon checks its local model catalog at execution time, logs a warning, and
omits the incompatible override.

Set it from the CLI with `--thinking-level` on `agent create` and `agent
update`, mirroring `--model`: the flag is a thin pass-through to the top-level
`thinking_level` field, and on update an empty string (`--thinking-level ""`)
clears it back to the runtime default. The CLI deliberately does not enumerate
the valid levels — they are runtime/model-specific (Claude currently uses
`low|medium|high|xhigh|max`; Pi uses
`off|minimal|low|medium|high|xhigh|max`; Codex values are discovered from the
runtime's model catalog). It forwards the token, the server applies the
provider's fixed-enum or safe-token gate, and the daemon performs the exact
model/level check. A runtime whose provider has no thinking concept rejects any
non-empty value with a 400.

`service_tier` is the matching first-class Codex speed control. Set it with
`--service-tier <catalog-id>` on create/update; use `--service-tier ""` on
update to clear it. The runtime model catalog owns both availability and
display copy (currently `priority`, shown as Fast). The server accepts safe
future Codex catalog IDs, while the daemon verifies the exact model/tier pair
before execution and omits a stale incompatible override. Agents without an
explicit model fail closed because the effective config.toml model is unknown.

### model vs custom_args

`model` is a first-class persisted column the daemon reads directly.
`custom_args` are raw provider CLI args. The CLI help notes that some providers
(codex app-server, openclaw) reject `--model` inside `custom_args` — but that is
documented CLI guidance, not a server-enforced invariant; nothing in the create
handler inspects `custom_args` for a model flag. Pi is stricter at invocation
time: `--thinking` in `custom_args` is filtered because the first-class
`thinking_level` field owns that flag and must be the only source of its value.

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
  endpoint (`multica agent env get`). It is gated to the **agent's own human
  owner** or a workspace **owner/admin**, and **agent actors are denied**
  regardless of the backing member's role — a running agent cannot read another
  agent's secrets, not even one its own human owns.
- Writing values after creation does NOT go through `agent update`. The generic
  update handler rejects any `custom_env` field with a 400 ("use PUT
  /api/agents/{id}/env"). Plaintext env writes are handled by
  `PUT /api/agents/{id}/env` (`multica agent env set`), which carries the same
  gate and writes an audit row.

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

Provider support is not uniform: Qwen Code accepts a managed `mcp_config` through a daemon-owned 0600 temporary JSON file passed with `--mcp-config`; it is removed when the run exits. Leave the field unset (`null`) to inherit Qwen Code native settings.

#### Workspace MCP servers

A workspace keeps a LIBRARY of MCP servers (workspace Settings → MCP, or
`multica workspace mcp list|add|update|remove`). Adding one there gives it to
NO agent — same shape as a workspace skill. It reaches an agent only when
someone assigns it:

```bash
multica workspace mcp list --output table        # find the server id
multica agent mcp add <agent-id> <server-id>     # give it to one agent
multica agent mcp disable <agent-id> <server-id> # stop sending it, keep the assignment
multica agent mcp remove <agent-id> <server-id>  # take it away
```

At claim time the effective set is:

| Layer | Reaches the agent when |
| --- | --- |
| runtime-local servers | always (the daemon merges the runtime's own file) |
| workspace servers | assigned to THIS agent and left enabled |
| the agent's own `mcp_config` | always; it WINS on a name collision |

Two consequences worth knowing before writing an agent's config: assigning a
shared server does not require re-listing it in `mcp_config` (they merge), and
`mcp_config` is now only about servers private to that agent — a
managed-but-empty `{}` no longer means anything about the workspace layer,
because nothing is inherited in the first place.

The stored entry is **write-only** — reads return the server's name and
transport, never urls, commands, headers, or env, for any role.

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
- `multica agent copy` — inserts a new agent row (a fork of an existing agent);
  the source is left untouched.
- `multica agent update --permission-mode/--public-to-*` — replaces the target
  agent's invocation allow-list; only the human agent owner may do this.
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
- "An agent allow-list edge shares the target's data." It does not: the edge
  authorizes invocation only, not env, MCP, chat, run-history, or unrelated
  resource reads.
- "Agent allow-lists are transitive." They are direct actor-id checks only.
- "An invalid `thinking_level`/`model` combo is caught at create." Only an
  unknown provider-level literal is — model-specific gaps fail at run time.
- "`set` and `add` are interchangeable for skills." `set` replaces all
  bindings; using it when you meant `add` silently removes capabilities.

## References

`references/creating-agents-source-map.md` maps every contract above to its
`file:line` on the current tree, the runtime effect, and a safe read-only
verification command.
