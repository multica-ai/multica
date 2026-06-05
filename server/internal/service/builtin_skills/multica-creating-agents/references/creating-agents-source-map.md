# Creating agents — source map

Evidence layer for `SKILL.md`. Every contract maps to `file:line` on the
current tree (branch `feat/builtin-skills`, latest `main` merged), the runtime
effect, and a safe read-only check. Line numbers were re-derived against this
tree — re-derive again if the files move, the surrounding context (not the
number) is the anchor.

## Verification

```bash
# Conformance eval for this skill (and the shared template invariants):
go test ./internal/service -run TestCreatingAgentsSkillCoversAgentCreationContracts
go test ./internal/service -run TestBuiltinSkillsConformToTemplate
```

## CLI entry points — `server/cmd/multica/cmd_agent.go`

| Contract | Line | Behavior | Safe check |
|---|---|---|---|
| Create flags: `name`, `description`, `instructions`, `runtime-id` | 159–162 | Registered create flags; `name`/`runtime-id` enforced in `runAgentCreate` | `multica agent create --help` |
| `runtime-config`, `model`, `custom-args` flags | 169–171 | `model` help: "Prefer this over passing --model in --custom-args"; `custom-args` help names codex/openclaw rejecting `--model` (CLI help only, not server-enforced) | `multica agent create --help` |
| Secret-safe env input: `custom-env`, `custom-env-stdin`, `custom-env-file` | 172–174 | `--custom-env` warns about shell history / `ps`; stdin and file modes keep secrets off the command line; mutually exclusive | `multica agent create --help` |
| `runAgentCreate` builds body + `POST /api/agents` | 409 | Only sets a body key when the flag `Changed`; posts to `/api/agents` (line 480) | read 409–491 |
| Body assembly: description/instructions/runtime-config/custom-args/custom-env/model | 432–474 | `resolveCustomEnv` (458) gates the three env channels; omitted flags are not sent | read 432–474 |
| `agent skills set` = replace-all | 814 | `PUT /api/agents/{id}/skills` (832); `--skill-ids ''` clears all (821) | `multica agent skills set --help` |
| `agent skills add` = additive | 839 | `POST /api/agents/{id}/skills/add` (860); requires ≥1 id (849) | `multica agent skills add --help` |
| `agent skills list` | 782 | reads bindings, no side effect | `multica agent skills list --help` |
| `agent env get` | 916 | `GET /api/agents/{id}/env` | `multica agent env get --help` |
| `agent env set` | 951 | `PUT /api/agents/{id}/env` with full `custom_env` map (965, 971) | `multica agent env set --help` |

Note: `--from-template` exists at line 168 and short-circuits to
`runAgentCreateFromTemplate` (line 498). It is intentionally NOT taught — the
template path is immature and out of scope for this skill.

## Create handler — `server/internal/handler/agent.go`

| Contract | Line | Behavior |
|---|---|---|
| `maxAgentDescriptionLength = 255` | 31 | Cap is 255 **Unicode code points** (comment: counted via `utf8.RuneCountInString`, matches Postgres `char_length`) |
| `AgentResponse` omits plaintext `custom_env` | 33–53 | Exposes only `has_custom_env` (52) and `custom_env_key_count` (53); comment cites MUL-2600 |
| `CreateAgentRequest` fields | 565–585 | `description`, `instructions`, `runtime_config`, `custom_env`, `custom_args`, `model`, `thinking_level` (plus name/avatar/visibility/mcp_config/max_concurrent_tasks) |
| `name` required | 623–625 | 400 "name is required" |
| `description` ≤ 255 code points | 627–629 | `utf8.RuneCountInString(req.Description) > maxAgentDescriptionLength` → 400 |
| `runtime_id` required | 631–633 | `if req.RuntimeID == ""` → 400 "runtime_id is required" |
| `runtime_id` must resolve in workspace | 642–658 | parsed + `GetAgentRuntimeForWorkspace`; unknown → 400 "invalid runtime_id" |
| `thinking_level` provider-level validation | 673–676 | `!agent.IsKnownThinkingValue(runtime.Provider, req.ThinkingLevel)` → 400; per-model gaps deferred to daemon (comment 669–672, MUL-2339) |
| Defaults: `{}` config/env, `[]` args | 688–701 | `RuntimeConfig`→`{}`, `CustomEnv`→`{}`, `CustomArgs`→`[]` when nil, before insert |
| `visibility` default | 635–636 | `if req.Visibility == "" { req.Visibility = "private" }` — access-control field, not the runtime prompt |
| `max_concurrent_tasks` default | 638–639 | `if req.MaxConcurrentTasks == 0 { req.MaxConcurrentTasks = 6 }` — scheduler cap |
| `mcp_config` null-skip on create | 704–705 | raw JSON copied through unless the body value is the literal `null` |
| `mcp_config` redacted on read | 54, 848–851 | `redactMcpConfig` sets `McpConfigRedacted=true`; a private agent read by a member also redacts (494, 509) |
| `CreateAgent` insert params | 708–722 | persists runtime_config, instructions, custom_env, custom_args, model, thinking_level, mcp_config, visibility, max_concurrent_tasks |
| `UpdateAgent` rejects `custom_env` | 910–913 | if `custom_env` present in body → 400 "use PUT /api/agents/{id}/env (or `multica agent env set`)" |
| `description` ≤ 255 on update too | 921–924 | same cap re-checked on update |

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
| Runtime payload | 1130–1143 | `TaskAgentData` carries `Instructions`, `Skills`, `CustomEnv`, `CustomArgs`, `Model`, `ThinkingLevel`, `McpConfig` (1130–1131, 1140) — confirms these are runtime-consumed; `description`, `visibility`, and `max_concurrent_tasks` are absent (not runtime-prompt fields) |

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
| `CreateAgent` INSERT | 730–736 | columns include `runtime_config, runtime_id, instructions, custom_env, custom_args, mcp_config, model, thinking_level` |
| `CreateAgentParams` | 739–756 | typed params: `RuntimeConfig []byte`, `Instructions string`, `CustomEnv []byte`, `CustomArgs []byte`, `Model pgtype.Text`, `ThinkingLevel pgtype.Text` |
| `UpdateAgent` SET | 2552–2566 | COALESCE updates of `runtime_config, instructions, custom_env, custom_args, model, thinking_level` — note `custom_env` is COALESCE-guarded but the handler rejects it before this query runs |
| `UpdateAgentCustomEnv` (called by the `UpdateAgentEnv` handler) | 2652 | `SET custom_env = $2` — the only write path for env values |
