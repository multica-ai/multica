# Creating agents — source map

Evidence layer for `SKILL.md`. Every contract maps to `file:line` on the
current tree, the runtime effect, and a safe read-only check. Line numbers were
re-derived against this tree — re-derive again if the files move, the
surrounding context (not the number) is the anchor.

## Verification

```bash
# Conformance eval for this skill (and the shared template invariants):
go test ./internal/service -run TestCreatingAgentsSkillCoversAgentCreationContracts
go test ./internal/service -run TestBuiltinSkillsConformToTemplate
```

## CLI entry points — `server/cmd/multica/cmd_agent.go`

| Contract | Line | Behavior | Safe check |
|---|---|---|---|
| Create flags: `name`, `description`, `instructions`, `runtime-id` | 160–163 | Registered create flags; `name`/`runtime-id` enforced in `runAgentCreate` | `multica agent create --help` |
| `runtime-config`, `model`, `thinking-level`, `service-tier`, `custom-args` flags | 164–168 | `model` help: "Prefer this over passing --model in --custom-args"; thinking and Codex service-tier values are thin catalog-owned pass-throughs, with exact model compatibility checked by the daemon; empty = runtime default | `multica agent create --help` |
| Secret-safe env input: `custom-env`, `custom-env-stdin`, `custom-env-file` | 169–171 | `--custom-env` warns about shell history / `ps`; stdin and file modes keep secrets off the command line; mutually exclusive | `multica agent create --help` |
| Secret-safe MCP input: `mcp-config`, `mcp-config-stdin`, `mcp-config-file` (create) | 172–174 | Same three-channel pattern as `custom-env`; `--mcp-config` warns about shell history / `ps`; value must be a JSON object or `null` | `multica agent create --help` |
| MCP flags on `agent update` | 200–202 | Same three channels on update; `--mcp-config null` clears. Unlike `custom_env`, `mcp_config` IS settable via update | `multica agent update --help` |
| `thinking-level` / `service-tier` flags on `agent update` | 189–190 | Thin pass-throughs; an explicit empty string clears the saved override and restores the runtime/local Codex default | `multica agent update --help` |
| `runAgentCreate` builds body + `POST /api/agents` | 533–624 | Only sets a body key when the flag `Changed`; posts to `/api/agents` (line 614) | read 533–624 |
| Body assembly: description/instructions/runtime-config/custom-args/custom-env/mcp-config/model/thinking-level/service-tier | 548–608 | `model`, `thinking_level`, and `service_tier` are `Changed`-gated pass-throughs; omitted flags are not sent | read the `runAgentCreate` body assembly |
| `runAgentUpdate` sends `thinking_level` / `service_tier` / `mcp_config` | 627–718 | Each override key is added only when its flag is `Changed`; `custom_env` is intentionally not a flag here | read the `runAgentUpdate` body assembly |
| `parseMcpConfig` / `resolveMcpConfig` helpers | 1210, 1238 | Validator (object-or-`null`, content-free errors) + three-channel resolver, mirroring `parseCustomEnv`/`resolveCustomEnv` | read 1210–1294 |
| `agent skills set` = replace-all | 916 | `PUT /api/agents/{id}/skills` (934); `--skill-ids ''` clears all (922–925) | `multica agent skills set --help` |
| `agent skills add` = additive | 941 | `POST /api/agents/{id}/skills/add` (962); requires ≥1 id (947–952) | `multica agent skills add --help` |
| `agent skills list` | 884 | reads bindings, no side effect | `multica agent skills list --help` |
| `agent env get` | 1018 | `GET /api/agents/{id}/env` (1028) | `multica agent env get --help` |
| `agent env set` | 1053 | `PUT /api/agents/{id}/env` with full `custom_env` map (1073) | `multica agent env set --help` |

Note: the CLI no longer exposes `--from-template`. The agent-template backend
still exists (registry `server/internal/agenttmpl/`, handler `agent_template.go`,
routes `GET /api/agent-templates` and `POST /api/agents/from-template`, plus the
`packages/core` client/query wrappers) but is currently orphaned plumbing with no
live caller: the removed CLI flag was its only non-test consumer, and onboarding
does NOT use it — `packages/views/onboarding/steps/step-agent.tsx` builds four
hardcoded local presets (i18n-resolved) and creates via plain `POST /api/agents`
(`createAgent`), never `POST /api/agents/from-template`. Do not treat the template
API as a supported agent-creation path. This skill teaches manual `agent create`
only.

## Create handler — `server/internal/handler/agent.go`

| Contract | Line | Behavior |
|---|---|---|
| `maxAgentDescriptionLength = 255` | 31 | Cap is 255 **Unicode code points** (comment: counted via `utf8.RuneCountInString`, matches Postgres `char_length`) |
| `AgentResponse` omits plaintext `custom_env` | 33–53 | Exposes only `has_custom_env` (52) and `custom_env_key_count` (53); comment cites MUL-2600 |
| `CreateAgentRequest` fields | 930–970 | Includes `model`, `thinking_level`, and Codex `service_tier` alongside the profile/runtime/permission inputs |
| `name` required | 623–625 | 400 "name is required" |
| `description` ≤ 255 code points | 627–629 | `utf8.RuneCountInString(req.Description) > maxAgentDescriptionLength` → 400 |
| `runtime_id` required | 631–633 | `if req.RuntimeID == ""` → 400 "runtime_id is required" |
| `runtime_id` must resolve in workspace | 642–658 | parsed + `GetAgentRuntimeForWorkspace`; unknown → 400 "invalid runtime_id" |
| `thinking_level` provider-level validation | 896–903 | `!agent.IsKnownThinkingValue(runtime.Provider, req.ThinkingLevel)` → 400; fixed providers use an enum, Codex/OpenCode use safe-token syntax, and per-model gaps are deferred to daemon (MUL-2339) |
| `service_tier` provider-level validation | `agent.go` create/update paths | Non-empty values are Codex-only safe tokens; exact per-model support is daemon-owned |
| Defaults: `{}` config/env, `[]` args | 688–701 | `RuntimeConfig`→`{}`, `CustomEnv`→`{}`, `CustomArgs`→`[]` when nil, before insert |
| `visibility` default | 635–636 | `if req.Visibility == "" { req.Visibility = "private" }` — access-control field, not the runtime prompt |
| `max_concurrent_tasks` default | 638–639 | `if req.MaxConcurrentTasks == 0 { req.MaxConcurrentTasks = 6 }` — scheduler cap |
| `mcp_config` null-skip on create | 704–705 | raw JSON copied through unless the body value is the literal `null` |
| `mcp_config` redacted on read | 54, 848–851 | `redactMcpConfig` sets `McpConfigRedacted=true`; a private agent read by a member also redacts (494, 509) |
| Qwen Code managed-MCP injection | `pkg/agent/qwen.go` | Non-null `mcp_config` is written to a daemon-owned 0600 temporary JSON file and passed with `--mcp-config`; the file is removed after the process exits, while `null` preserves native inheritance. |
| Random emoji avatar default | `agent_avatar.go` 11–32; `agent.go` 1127–1133 | Omitted, empty, or whitespace-only `avatar_url` becomes a cryptographically selected `emoji:<glyph>` sentinel; explicit values are preserved. The template handler uses the same helper at `agent_template.go` 458. |
| `CreateAgent` insert params | `agent.go` create path | Persists avatar_url, runtime_config, instructions, custom_env, custom_args, model, thinking_level, service_tier, mcp_config, visibility, max_concurrent_tasks |
| `UpdateAgent` rejects `custom_env` | 910–913 | if `custom_env` present in body → 400 "use PUT /api/agents/{id}/env (or `multica agent env set`)" |
| `UpdateAgent` persists / clears `mcp_config` | 944–948, 1060–1061 | Tri-state from the raw body: key omitted → no change; literal `null` → `ClearAgentMcpConfig`; object → replace. No 400 like `custom_env` — `mcp_config` IS updatable here |
| `description` ≤ 255 on update too | 921–924 | same cap re-checked on update |

## Runtime model/thinking discovery — `server/pkg/agent/{models,thinking}.go`

| Contract | Line | Behavior |
|---|---|---|
| Codex model-list entry point | `models.go` 94–103 | `ListModels("codex")` uses cached daemon-local discovery instead of returning the fallback catalog unconditionally |
| Codex fallback catalog | `models.go` 301–354 | Used for Codex <0.122.0 and failed/malformed discovery; includes current verified visible models plus legacy `gpt-5.3-codex`, with a separate `Thinking` catalog on every model |
| Codex discovery version gate | `thinking.go` 280, 306–337 | `codex debug models --bundled` is used only for parseable versions ≥0.122.0; unsupported versions and command/parse/empty failures return the static model + thinking fallback |
| Codex catalog projection | `thinking.go` `parseCodexModelCatalog` | Hidden models are excluded; visible model, reasoning, and `service_tiers` metadata are preserved |
| Per-model thinking validation | `thinking.go` 547–640 | `ValidateThinkingLevel` accepts only values in the explicit model's `Thinking.SupportedLevels`; an empty Codex model fails closed because its effective `config.toml` model is unknown |
| Dynamic Codex token gate | `thinking.go` 642–710 | Server persistence accepts syntactically safe Codex tokens so new catalog values do not require a Multica release; exact support remains a daemon-local per-model check |
| Per-model service-tier validation | `thinking.go` `ValidateServiceTier` | Accepts only a tier advertised for the explicit Codex model; empty model fails closed because config.toml is unknown |
| Daemon invalid-combination handling | `internal/daemon/daemon.go` 3860–3892 | Before execution, invalid `(provider, model, thinking_level)` combinations log a warning and omit the override rather than failing the task |

## Env endpoint — `server/internal/handler/agent_env.go`

| Contract | Line | Behavior |
|---|---|---|
| `authorizeAgentEnv` gate | 66 | loads agent, then applies the two checks below |
| Agent actors denied | 80–84 | `if actorType == "agent"` → 403 "agents may not access env management endpoints" (MUL-2600 impersonation guard) |
| Owner/admin only | 86 | `requireWorkspaceRole(..., "owner", "admin")` |

## Routes — `server/cmd/server/router.go`

| Contract | Line | Behavior |
|---|---|---|
| `GET /env` | 603 | `h.GetAgentEnv` (plaintext read, gated) |
| `PUT /env` | 604 | `h.UpdateAgentEnv` (full-map overwrite, gated) |

## Claim-time injection — `server/internal/handler/daemon.go`

| Contract | Line | Behavior |
|---|---|---|
| Fresh agent re-read on claim | 1109–1111 | `GetAgent(task.AgentID)` — claim uses persisted fields, not create output |
| Workspace skills FIRST | 1115 | `skills := h.TaskService.LoadAgentSkills(...)` |
| Built-ins appended | 1116 | `skills = append(skills, h.TaskService.BuiltinSkills()...)` |
| Runtime payload | `daemon.go` `TaskAgentData` | Carries `Instructions`, `Skills`, `CustomEnv`, `CustomArgs`, `Model`, `ThinkingLevel`, `ServiceTier`, and `McpConfig`; metadata-only fields remain absent |

## Skill loading — `server/internal/service/task.go`

| Contract | Line | Behavior |
|---|---|---|
| `LoadAgentSkills` | 1685 | `ListAgentSkills` + per-skill `ListSkillFiles` → content + supporting files for execution |

## Built-in skills — `server/internal/service/builtin_skills.go`

| Contract | Line | Behavior |
|---|---|---|
| `go:embed builtin_skills` | 10–11 | skills embedded at compile time |
| `loadBuiltinSkill` | 45 | reads `<name>/SKILL.md` (47) + walks sibling files into `Files` (56–68) |

## Persisted columns — `server/pkg/db/generated/agent.sql.go`

| Contract | Line | Behavior |
|---|---|---|
| `CreateAgent` INSERT | generated from `queries/agent.sql` | columns include `runtime_config, runtime_id, instructions, custom_env, custom_args, mcp_config, model, thinking_level, service_tier` |
| `CreateAgentParams` | generated from `queries/agent.sql` | typed params include nullable `Model`, `ThinkingLevel`, and `ServiceTier` |
| `UpdateAgent` SET | generated from `queries/agent.sql` | COALESCE updates include model/thinking/service tier; dedicated clear queries restore each nullable override |
| `UpdateAgentCustomEnv` (called by the `UpdateAgentEnv` handler) | 2652 | `SET custom_env = $2` — the only write path for env values |
