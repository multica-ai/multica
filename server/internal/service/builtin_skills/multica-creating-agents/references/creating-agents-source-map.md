# Creating agents — source map

Evidence layer for `SKILL.md`; `file:line` anchors on the current tree,
re-derived (not numbers) when files move. Line numbers re-derived on tree
changes — anchor on surrounding context.

## Verification

```bash
go test ./internal/service -run TestCreatingAgentsSkillCoversAgentCreationContracts
go test ./internal/service -run TestBuiltinSkillsConformToTemplate
```

## CLI entry points — `server/cmd/multica/cmd_agent.go`

| Contract | Line | Behavior |
|---|---|---|
| Create flags: `name`, `description`, `instructions`, `runtime-id` | 160–163 | registered |
| `runtime-config`, `model`, `thinking-level`, `service-tier`, `custom-args` | 164–168 | pass-through |
| Secret-safe env: `custom-env`, `custom-env-stdin`, `custom-env-file` | 169–171 | stdin/file keep secrets out of ps |
| Secret-safe MCP: `mcp-config`, `mcp-config-stdin`, `mcp-config-file` (create) | 172–174 | same |
| MCP flags on `agent update` | 200–202 | `--mcp-config null` clears |
| `thinking-level`/`service-tier` on update | 189–190 | thin pass-throughs; `""` clears |
| `max-concurrent-tasks` validation | cmd_agent.go 179, 208; cmd_agent_validation.go | 1–50 |
| `runAgentCreate` builds body + `POST /api/agents` | 533–628 | key set only when flag provided |
| `runAgentUpdate` sends `thinking_level` | 630–725 | pass-through |
| `--skill-ids ''` clears all | 947 | `PUT /api/agents/{id}/skills` (940) |
| `POST /api/agents/{id}/skills/add` | 968 | ≥1 ids required |
| `GET /api/agents/{id}/env` (1034); `PUT /api/agents/{id}/env` (1079) | 1024, 1059 | full-map overwrite |

## Copy command — `server/cmd/multica/cmd_agent_copy.go`

| Contract | Line | Behavior |
|---|---|---|
| `agentCopyCmd` + flag registrar | 21, 47, 54 | own file |
| Reads source via `GET /api/agents/<id>` | 95 | no dedicated API |
| Same-runtime vs cross-runtime | 114, 187 | `sameRuntime` copies `model`/`thinking_level`/`service_tier` |
| Concurrency copy compatibility | `runAgentCopy`, `copiedAgentMaxConcurrentTasks` | explicit override rejected out of 1–50 |
| Skills in create transaction | 239 | `skill_ids` bound in same insert |
| Secrets never copied | 240–266 | `custom_env`/`mcp_config`/`runtime_config` only when explicitly set |

## Create handler — `server/internal/handler/agent.go`

| Contract | Line | Behavior |
|---|---|---|
| Description cap 255 | 627–629 | `utf8.RuneCountInString(req.Description) > 255` → 400 |
| `runtime_id` must resolve in workspace | 642–658 | `GetAgentRuntimeForWorkspace` |
| `thinking_level` validation | `agent.go` create/update | `!agent.IsKnownThinkingValue(runtime.Provider, req.ThinkingLevel)` → 400; fixed-vocabulary providers use an enum (Pi: `off|minimal|low|medium|high|xhigh|max`), Codex/OpenCode safe-token; per-model gaps deferred to daemon (MUL-2339) |
| `thinking_level` rejection copy | `agent.go` `thinkingLevelRejection`/`existingThinkingLevelRejection` | splits "runtime has no reasoning control" from "unrecognised token"; both point at `thinking_level=""` (MUL-5770) |
| `service_tier` provider-level validation | create/update paths | non-empty → 400 when unsupported |
| Defaults `{}`/`[]` | 688–701 | `RuntimeConfig`→`{}`, `CustomEnv`→`{}`, `CustomArgs`→`[]` |
| `visibility` default | 635–636 | `if req.Visibility == "" { req.Visibility = "private" }` |
| `max_concurrent_tasks` create/update validation | agent.go; agent_validation.go | 1–50; update omission preserves |
| `mcp_config` null-skip on create | 704–705 | raw JSON copied unless literal `null` |
| `mcp_config` redacted on read | 54, 848–851 | `redactMcpConfig` → `McpConfigRedacted=true` |
| Qwen managed-MCP | `pkg/agent/qwen.go` | non-null config → daemon-owned 0600 temp file |
| Random emoji avatar | `agent_avatar.go` 11–32; `agent.go` 1127–1133 | omitted/empty/whitespace-only → cryptographically selected `emoji:<glyph>` |
| `UpdateAgent` rejects `custom_env` | 910–913 | 400 "use PUT /api/agents/{id}/env" |
| `UpdateAgent` mcp tri-state | 944–948, 1060–1061 | omitted/no change, `null`/clear, object/replace |
| Description cap on update | 921–924 | same 255 check |

Model/thinking catalog: `server/pkg/agent/{models,thinking}.go` —
`ListModels("codex")` (models.go 94–103), `ValidateThinkingLevel`
(thinking.go 547–640, accepts only known values), dynamic Codex token gate
(642–710, syntactically safe tokens), `ThinkingControlSupported` (runtime
reasoning capability), `ValidateServiceTier` (tier ids), daemon
invalid-combination handling (daemon.go 3860–3892, omits incompatible
override at execution).

## Env endpoint — `server/internal/handler/agent_env.go`

| Contract | Line | Behavior |
|---|---|---|
| `authorizeAgentEnv` gate | 76 | loads agent, two checks |
| Agent actors denied | 90–94 | `if actorType == "agent"` → 403 |
| Owner/ws owner-admin | 96–103 | `requireWorkspaceRole(..., "owner", "admin", "member")` + owner match |
| `canManageAgentEnv` | 120 | owner/admin or `agent.owner_id == member.user_id` |

## Routes — `server/cmd/server/router.go`

`GET /env` (603) → `h.GetAgentEnv`; `PUT /env` (604) → `h.UpdateAgentEnv`.

## Claim-time injection — `server/internal/handler/daemon.go`

Fresh agent re-read (1109–1111 `GetAgent(task.AgentID)`); workspace skills
FIRST (1115 `LoadAgentSkills`), built-ins appended (1116
`append(skills, h.TaskService.BuiltinSkills()...)`). `TaskAgentData.CustomEnv`
from `server/internal/service/task.go` `LoadAgentSkills` (1685):
`ListAgentSkills` + per-skill `ListSkillFiles`; built-ins embedded
(`server/internal/service/builtin_skills.go` 10–11), `<name>/SKILL.md` (45) + sibling files into `Files`
(47, 56).

## Persisted columns — `server/pkg/db/generated/agent.sql.go`

`CreateAgent` INSERT / `CreateAgentParams` / `UpdateAgent` SET generated from
`queries/agent.sql` (nullable model/thinking/service_tier COALESCE updates);
`UpdateAgentCustomEnv` (2652, called by `UpdateAgentEnv`) `SET custom_env = $2`.
