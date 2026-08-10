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
| `max-concurrent-tasks` flags + validation | `cmd_agent.go` 179, 208; `cmd_agent_validation.go` 5–20 | Shared CLI helper enforces 1–50; create/update call it before their HTTP mutation and omitted create flags stay absent | `multica agent create --help`; `multica agent update --help` |
| `runAgentCreate` builds body + `POST /api/agents` | 533–628 | Only sets a body key when the flag `Changed`; validates `max_concurrent_tasks` at 605–611, then posts to `/api/agents` (617) | read 533–628 |
| Body assembly: description/instructions/runtime-config/custom-args/custom-env/mcp-config/model/thinking-level/service-tier | 548–611 | `model`, `thinking_level`, and `service_tier` are `Changed`-gated pass-throughs; omitted flags are not sent | read the `runAgentCreate` body assembly |
| `runAgentUpdate` sends `thinking_level` / `service_tier` / `mcp_config` | 630–725 | Each override key is added only when its flag is `Changed`; `max_concurrent_tasks` is range-checked at 693–699; `custom_env` is intentionally not a flag here | read the `runAgentUpdate` body assembly |
| `parseMcpConfig` / `resolveMcpConfig` helpers | 1216, 1244 | Validator (object-or-`null`, content-free errors) + three-channel resolver, mirroring `parseCustomEnv`/`resolveCustomEnv` | read 1216–1301 |
| `agent skills set` = replace-all | 922 | `PUT /api/agents/{id}/skills` (940); `--skill-ids ''` clears all (928–931) | `multica agent skills set --help` |
| `agent skills add` = additive | 947 | `POST /api/agents/{id}/skills/add` (968); requires ≥1 id (953–958) | `multica agent skills add --help` |
| `agent skills list` | 890 | reads bindings, no side effect | `multica agent skills list --help` |
| `agent env get` | 1024 | `GET /api/agents/{id}/env` (1034) | `multica agent env get --help` |
| `agent env set` | 1059 | `PUT /api/agents/{id}/env` with full `custom_env` map (1079) | `multica agent env set --help` |

## Copy command — `server/cmd/multica/cmd_agent_copy.go`

| Contract | Line | Behavior | Safe check |
|---|---|---|---|
| `agentCopyCmd` (`copy <source-agent-id>`) + flag registrar | 21, 47, 54 | Own file with its own `init()` so `cmd_agent.go` line refs stay stable; `registerAgentCopyFlags` is shared with the tests | `multica agent copy --help` |
| Reads source via `GET /api/agents/<id>` | 95 | Composes over existing endpoints — no dedicated copy API | read `runAgentCopy` |
| Same-runtime vs cross-runtime rule | 114, 187 | `sameRuntime` copies `model`/`thinking_level`/`service_tier`; a different `--runtime-id` drops them and requires `--model` (empty allowed) | `multica agent copy --help` |
| Concurrency copy compatibility | `runAgentCopy`, `copiedAgentMaxConcurrentTasks` | Explicit `--max-concurrent-tasks` is validated before any request; valid source values are copied, while historical values outside 1–50 are omitted so create defaults to 6 | read the concurrency body assembly |
| Skills copied in the create transaction | 239 | Source skill ids sent as `skill_ids`, bound in the same `POST /api/agents` tx (267); `--no-skills` opts out | read `runAgentCopy` |
| Secrets never copied | 240–266 | `custom_env`/`mcp_config`/`runtime_config` set only from explicit secret-safe flags, never read from the source | `multica agent copy --help` |

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
| `thinking_level` provider-level validation | `agent.go` create/update paths | `!agent.IsKnownThinkingValue(runtime.Provider, req.ThinkingLevel)` → 400; fixed-vocabulary providers use an enum (Pi: `off|minimal|low|medium|high|xhigh|max`), Codex/OpenCode use safe-token syntax, and per-model gaps are deferred to daemon (MUL-2339) |
| `thinking_level` rejection copy | `agent.go` `thinkingLevelRejection` / `existingThinkingLevelRejection` | Splits "runtime has no reasoning control" from "unrecognised token" so a runtime-capability 400 does not read as a typo; both carry-over branches point at `thinking_level=""` (MUL-5770) |
| `service_tier` provider-level validation | `agent.go` create/update paths | Non-empty values are Codex-only safe tokens; exact per-model support is daemon-owned |
| Defaults: `{}` config/env, `[]` args | 688–701 | `RuntimeConfig`→`{}`, `CustomEnv`→`{}`, `CustomArgs`→`[]` when nil, before insert |
| `visibility` default | 635–636 | `if req.Visibility == "" { req.Visibility = "private" }` — access-control field, not the runtime prompt |
| `max_concurrent_tasks` create/default validation | `agent.go`; `agent_validation.go`; `internal/agentconfig/concurrency.go` | Shared 1–50 validator; a missing or explicit `null` field defaults to 6, while an explicitly supplied numeric 0/out-of-range value returns 400 |
| `max_concurrent_tasks` update validation | 1660–1666 | Omission preserves the existing value; a supplied value outside 1–50 returns 400 before persistence |
| `mcp_config` null-skip on create | 704–705 | raw JSON copied through unless the body value is the literal `null` |
| `mcp_config` redacted on read | 54, 848–851 | `redactMcpConfig` sets `McpConfigRedacted=true`; a private agent read by a member also redacts (494, 509) |
| Qwen Code managed-MCP injection | `pkg/agent/qwen.go` | Non-null `mcp_config` is written to a daemon-owned 0600 temporary JSON file and passed with `--mcp-config`; the file is removed after the process exits, while `null` preserves native inheritance. |
| Random emoji avatar default | `agent_avatar.go` 11–32; `agent.go` 1127–1133 | Omitted, empty, or whitespace-only `avatar_url` becomes a cryptographically selected `emoji:<glyph>` sentinel; explicit values are preserved. The template handler uses the same helper at `agent_template.go` 458. |
| `CreateAgent` insert params | `agent.go` create path | Persists avatar_url, runtime_config, instructions, custom_env, custom_args, model, thinking_level, service_tier, mcp_config, visibility, max_concurrent_tasks |
| `UpdateAgent` rejects `custom_env` | 910–913 | if `custom_env` present in body → 400 "use PUT /api/agents/{id}/env (or `multica agent env set`)" |
| `UpdateAgent` persists / clears `mcp_config` | 944–948, 1060–1061 | Tri-state from the raw body: key omitted → no change; literal `null` → `ClearAgentMcpConfig`; object → replace. No 400 like `custom_env` — `mcp_config` IS updatable here |
| `description` ≤ 255 on update too | 921–924 | same cap re-checked on update |

## Create-from-template handler — `server/internal/handler/agent_template.go`

| Contract | Line | Behavior |
|---|---|---|
| `max_concurrent_tasks` default + validation | `CreateAgentFromTemplate`; `defaultAndValidateAgentMaxConcurrentTasks` | Uses the same helper as manual create: omission or `null` defaults to 6; explicit numeric values outside 1–50 return 400 |

## Runtime model/thinking discovery — `server/pkg/agent/{models,thinking}.go`

| Contract | Line | Behavior |
|---|---|---|
| Codex model-list entry point | `models.go` 94–103 | `ListModels("codex")` uses cached daemon-local discovery instead of returning the fallback catalog unconditionally |
| Codex fallback catalog | `models.go` 301–354 | Used for Codex <0.122.0 and failed/malformed discovery; includes current verified visible models plus legacy `gpt-5.3-codex`, with a separate `Thinking` catalog on every model |
| Codex discovery version gate | `thinking.go` 280, 306–337 | `codex debug models --bundled` is used only for parseable versions ≥0.122.0; unsupported versions and command/parse/empty failures return the static model + thinking fallback |
| Codex catalog projection | `thinking.go` `parseCodexModelCatalog` | Hidden models are excluded; visible model, reasoning, and `service_tiers` metadata are preserved |
| Pi RPC model/thinking discovery | `models.go` `discoverPiModelsRPC` / `piThinkingFromRPCModel` | Starts an ephemeral `pi --mode rpc --no-session` process, requests `get_state` + `get_available_models`, preserves extension-registered providers, marks the current model as Default, and mirrors Pi's exact per-model `reasoning` / `thinkingLevelMap` rules. Older/forked Pi falls back to `--list-models` with no guessed thinking catalog. |
| Pi invocation effort | `pi.go` `buildPiArgs` / `piBlockedArgs` | A non-empty persisted value becomes `--thinking <level>`; custom `--thinking` flags are filtered so the first-class field is the sole owner. |
| Per-model thinking validation | `thinking.go` `ValidateThinkingLevel` | Accepts only values in the explicit model's `Thinking.SupportedLevels`; Pi empty-model validation resolves to the RPC current-model Default, while an empty Codex model fails closed because its effective `config.toml` model is unknown. |
| Dynamic Codex token gate | `thinking.go` `IsKnownThinkingValue` | Server persistence accepts syntactically safe Codex/OpenCode tokens so new catalog values do not require a Multica release; Pi instead uses its fixed seven-token CLI vocabulary. Exact support remains a daemon-local per-model check. |
| Runtime reasoning capability | `thinking.go` `ThinkingControlSupported` | True for the fixed-enum providers (including Pi) plus Codex/OpenCode. False for runtimes with no effort dial on the surface the daemon drives — Hermes' ACP adapter never carries `reasoning_config` onto a session, so its CLI-level `agent.reasoning_effort` cannot be reached from Multica (verified against Hermes Agent v0.18.2, MUL-5770) |
| Per-model service-tier validation | `thinking.go` `ValidateServiceTier` | Accepts only a tier advertised for the explicit Codex model; empty model fails closed because config.toml is unknown |
| Daemon invalid-combination handling | `internal/daemon/daemon.go` 3860–3892 | Before execution, invalid `(provider, model, thinking_level)` combinations log a warning and omit the override rather than failing the task |

## Env endpoint — `server/internal/handler/agent_env.go`

| Contract | Line | Behavior |
|---|---|---|
| `authorizeAgentEnv` gate | 76 | loads agent, then applies the two checks below |
| Agent actors denied | 90–94 | `if actorType == "agent"` → 403 "agents may not access env management endpoints" (MUL-2600 impersonation guard); runs FIRST, so an agent is denied even when its backing human owns the target agent |
| Agent owner or ws owner/admin | 96–103 | `requireWorkspaceRole(..., "owner", "admin", "member")` then `canManageAgentEnv` → 403 otherwise (MUL-5438) |
| `canManageAgentEnv` predicate | 120 | workspace owner/admin, or `agent.owner_id == member.user_id`; a NULL `owner_id` never matches |

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
