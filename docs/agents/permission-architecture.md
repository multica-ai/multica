# Permission architecture — the real structure & completeness register

**Read this together with [`permission-system.md`](./permission-system.md).** That doc is the
*behavioral* map ("what is enforced live today vs. off by default", row by row). **This**
doc is the *structural* map: what permission **models** actually exist, which gates are
authored through the 5 permission interfaces vs. which live **only in code**, and the
decision to remove Persona. If you change the permission model itself (add an interface,
move a gate into the tool-policy chain, remove Persona), update this file in the same PR.

Line numbers drift; this doc cites files + functions. Grep the function name if a line moved.

---

## 1. The mistake this doc exists to prevent

It is tempting to believe Multica has **one** permission model — the unified tool-policy
chain (Allow / Ask / Deny / Inherit across **Workspace › Runtime › Agent › Group › User**),
which is what the settings UI shows. **That belief is false.** The tool-policy chain (the
"5 interfaces") is a thin, deliberate slice. The **majority** of real access control lives
in code, spread across **four parallel models** that the 5 interfaces neither show nor
control. Section 4 is the complete register of those code-only gates. If you are building
"one place to see and control all permissions", that register is your backlog.

---

## 2. The intended model — the 5 permission interfaces

The unified tool-policy chain is the model we want **everything** to converge on.

- **The 5 interfaces = the 5 layers** of the chain: `workspace / runtime / agent / group / user`
  (`server/internal/cerebro/toolpolicy/chain.go:62-70`). These are the 5 settable surfaces
  the UI renders (`packages/cerebro-tool-policy` `ToolPolicyTable`, `view` prop →
  `VIEW_EDIT_LAYER`).
- **Resolver:** `toolpolicy.Resolve` (pure fold, `chain.go:153`) / `Store.Resolve`
  (DB, `store.go:95`). Folds base→ceiling, most-restrictive-wins; `Inherit`/absent =
  pass-through; **Base default = Allow** (`chain.go:155-157`).
- **FIR-2351 unification:** `Resolve` and `ResolveMemberOverride` are now thin wrappers
  over one function, `toolpolicy.ResolveWithMode(mode Mode, in Input)` — `ModeHardFloor`
  runs `Resolve`'s body, `ModeOpenable` runs `ResolveMemberOverride`'s body. This closes
  the class of bug that caused the on_behalf_of parity gap above (two hand-synced
  algorithms drifting apart): there is now one function body per mode, checked
  byte-for-byte equal to the pre-unification functions by
  `TestResolveWithMode_HardFloorMatchesResolve` / `_OpenableMatchesResolveMemberOverride`.
  `Store.ResolveGeneral` calls `ResolveWithMode` internally; no other call site changed.
  The "contract" step (deleting the two wrapper names and moving every `Resolve(...)` call
  site onto `ResolveWithMode` directly) is deferred to a follow-up PR.
- **Workspace Deny = openable default (FIR-2351, product decision 2026-07-06):** REVERSES
  the earlier "authored vs default deny" plan item — a workspace-level setting, authored or
  not, is a DEFAULT under `ModeOpenable`, and an explicit Allow authored at the Agent, Group,
  or User layer opens it (safety lives on the write path: owner/admin + the two
  Manage-permissions capabilities). Concretely, and ALL behind `cerebro_member_override`:
  (1) an explicit Agent-layer setting may open a workspace-authored default when no explicit
  Group/User row caps it (member rows stay the agent's ceiling; Runtime/on_behalf_of/System
  stay tighten-only); (2) `Store.Table`'s Effective column resolves with the same mode the
  gates enforce (`tableRowMode` keeps credential.*/repo-verbed/agent-browser rows on the
  tighten-only display); (3) `ConnectionEndpointEffective` treats an explicit workspace-layer
  row as the connection's workspace-authored default, decided after per-actor rows and before
  `default_access` (per-actor explicit Deny still revokes instantly; on_behalf_of stays
  Deny-only). Floors never consult the flag and are unchanged.
- **"Disable" — a per-permission hard floor (FIR-2351 follow-up, product decision
  2026-07-06):** a third workspace-only setting, `SettingDisable`, is the mirror image of
  the openable default above — a workspace row set to Disable (not Deny) is an unopenable
  floor for that one permission: no Group/User/Agent Allow can loosen it, Runtime/Agent/
  on_behalf_of/System can still tighten (no-op, it's already tightest). `resolveOpenable`
  short-circuits Stage A specificity and the Agent-opening exception on
  `workspaceDisabled`; `normalizeDisable` folds `SettingDisable` → `SettingDeny` before it
  reaches `rank()`, so `Effective.Setting` never leaks anything but Allow/Ask/Deny.
  `ConnectionEndpointEffective` returns Deny immediately on a workspace Disable, ahead of
  any per-actor row and regardless of the flag. Only valid at `LayerWorkspace`
  (`validSetting` + DB CHECK `cerebro_tool_policy_disable_workspace_only`, migration
  `9122`); workspace-layer writes already require owner/admin, so Disable needs no new
  write gate. UI: only the workspace-layer decision control offers it.
- **Member-override resolver (FIR-2175/FIR-3062, flag `cerebro_member_override`, default ON):**
  `toolpolicy.ResolveMemberOverride` (pure, `chain.go:245`) is a two-stage variant — Stage A
  resolves the human layers `Workspace › Group › User` by **specificity** (most specific wins,
  so a member's own Allow can OPEN what their group denied — it can LOOSEN, not only tighten),
  Stage B lets `Runtime`/`Agent`/`on_behalf_of`/`System` only tighten that member ceiling
  (`on_behalf_of` — the delegated task initiator, FIR-2441 — closed a parity gap with `Resolve`:
  it now tightens under both resolvers identically, never loosens either). The **general gate
  only** dispatches to it through `Store.ResolveGeneral(ctx, q, memberOverride)` (`store.go`),
  where `memberOverride` is the workspace flag read by the gate handler
  (`runtime.memberOverrideEnabled` / `handler.daemonMemberOverrideEnabled`, both fail-to-OFF).
  Flag OFF ⇒ `ResolveGeneral` is byte-for-byte `Resolve`. **SECURITY:** because it can loosen,
  it MUST NOT govern any deny-by-default floor — credentials, the OS sandbox, repo checkout,
  the repo-approval cap all keep calling `Resolve` directly and never reach `ResolveGeneral`.
- **Backing table:** `cerebro_tool_policy` — `(workspace_id, tool_key, layer, subject_id,
  resource_pattern, setting)` (migrations `9042` + `9052` workspace layer + `9054`
  resource_pattern + `9122` `disable` setting). `setting ∈ inherit|allow|ask|deny|disable`,
  with `disable` DB-constrained to `layer = 'workspace'` only.
- **Read model for UI:** `toolpolicy/table.go:101` (`Store.Table`) — one row per capability
  with per-layer settings, `Effective`, and `CappedByGroups` blame.
- **Write surface:** `PUT/DELETE /api/workspaces/{id}/tool-policy` (`toolpolicy/handler.go`),
  gated by `Handler.RequireToolPolicyWritePolicy` (`group_permissions_cerebro.go`). The base
  case is still the **code-only** owner/admin role check (section 4) — but two carve-outs
  delegate specific writes off that hardcoded role: a `credential.*` key routes to
  `manage_credential_access` (FIR-1479, `Resolve`+Base=Allow, tighten-only); a LayerUser row on
  any OTHER key routes to `manage_group_overrides` / `manage_workspace_overrides` (FIR-2351,
  `ResolveOptIn` — **OFF by default, opt-in only**; Base=Allow would be backwards here since
  the whole point is a permission nobody holds until granted). Group-scope reaches a user
  sharing a group with the actor; workspace-scope reaches any OTHER real member of THIS
  workspace (a UUID valid only in a different workspace is rejected via
  `GetMemberByUserAndWorkspace`). **Neither may ever target the actor's own row**
  (`toolpolicy.CanAuthorDelegatedOverride`) — admin is the one deliberate, documented exception
  and bypasses both, including on its own row. **FIR-2351 follow-up (2026-07-06):** the two
  capabilities are titled `Manage permissions` / `Manage group permissions` in the app (keys
  unchanged); a **Group- or Agent-layer** row on a non-credential key now routes to
  `manage_workspace_overrides` too (`cerebroRequireManagePermissionsPolicy`) so a Manage
  permissions holder can grant a group or one agent access that opens a workspace default
  Deny; and group-scope resolves once per actor∩target shared group with `group_id` threaded
  into `RequestContext.ArgValues`, so an `arg_allowlist` WHEN condition on the capability row
  pins `Manage group permissions` to specific group(s). Workspace/Runtime/System-layer writes
  (on a non-credential key) stay admin-only.
- **CLI surface (FIR-1609):** `multica permissions explain|set|clear`
  (`server/cmd/multica/cerebro_permissions.go`) wraps the read model + write surface above —
  `explain` (GET) prints per-tool `Effective` + `DecidedBy`/`CappedBy`/reason + group blame to
  answer "why is this agent+member blocked"; `set`/`clear` (PUT/DELETE) author/remove one
  Allow/Ask/Deny rule at one layer, optional WHEN/CEL condition. Same admin-only server gate.

**What actually routes through these interfaces today (Class A — the whole list):**

| Gate | Where | Live? |
|---|---|---|
| agent-browser unix-socket (`tools:agent-browser`, **Base=Deny**) | `daemon_tool_policy_cerebro.go:281` | live |
| repo checkout (`repo.checkout`, Base=Allow) | `daemon.go:3059` `CheckDaemonRepoCapability` | live |
| `create_local_runtime` | `group_permissions_cerebro.go:186` | live |
| `manage_connections` | `group_permissions_cerebro.go:237` (+ router middleware) | live |
| connection per-tool Deny/Ask | `table_connection.go:305` `ConnectionToolEffective` | live |
| mention `trigger_other_agent` (layered over a code baseline) | `mentiongate/gate.go:92` | live |
| general gateway tool calls (`guardToolCallViaPolicy`, via `ResolveGeneral`) | `approval_gate.go:314` | **off by default** (env `CEREBRO_APPROVAL_GATE_ENABLED` + `MODE=toolpolicy`) |
| local-CLI tool calls (Claude/Codex/Cursor/Gemini, via `ResolveGeneral`) | `daemon_tool_policy_cerebro.go:68` | **off by default** (flags `cerebro_local_tool_policy`(+`_enforce`)) |

Both **general** gates resolve through `Store.ResolveGeneral`, so when `cerebro_member_override`
(FIR-2175, default OFF) is on for the workspace they apply the member-override model; OFF keeps
them identical to `Resolve`. The **floor** gates in this table — agent-browser unix-socket
(Base=Deny), repo checkout, connection per-tool — keep calling `Resolve` directly and are NOT
affected by that flag.

Everything below in section 4 is **not** on this list — it is a separate model.

---

## 2a. The canonical inventory — the 57 platform capabilities

There is already an authoritative, code-owned inventory of platform actions:
`server/internal/cerebro/platformcatalog/catalog.go` (`var catalog`, exposed via `All()`).
It was built from the hardcoded-policy audit (Jesper, 2026-05-31) **specifically so the
buried code gates become settable instead of invisible**. Each entry carries: `Key` (the
`tool_key` an `cerebro_tool_policy` row binds to), `Title`, `Category`, `Description`, `Ops`
(the real HTTP routes it covers), and/or `Evidence` (file:line of the hardcoded check when
it is not a single route). A capability MUST have at least one of `Ops` or `Evidence` — that
is the traceability tripwire.

**This catalog is the canonical scope of platform actions.** It is **57 capabilities** in 17
categories. It is surfaced in the tool-policy table **only when the `cerebro_platform_capabilities`
feature flag is on (default OFF)** and the server gate `toolpolicy.PlatformCapabilitiesEnabled`
passes — so today it is an **inventory, not an enforcement point**. Wiring it on is part of FIR-1496.

| Category | Capabilities (`tool_key`) |
|---|---|
| Issues | `create_issue`, `update_issue`, `delete_issue`, `rerun_issue`, `subscribe_issue`, `manage_labels`, `manage_share_tokens`, `manage_issue_recurrence` |
| Comments | `add_comment`, `update_comment` |
| Autopilots | `create_autopilot`, `trigger_autopilot`, `autopilot_scope`, `autopilot_webhook` ⚠ |
| Artifacts | `manage_artifacts`, `manage_notes`, `manage_note_types` |
| Agents | `create_agent`, `update_agent`, `trigger_other_agent`, `schedule_agent_wakeup`, `manage_agent_passes`, `manage_work_sessions` |
| Runtimes | `manage_runtime`, `manage_runtime_tool_access`, `manage_runtime_accounts`, `manage_cloud_runtime`, `create_runtime`, `create_local_runtime`, `use_other_runtime`, `daemon_runtime_callback` ⚠ |
| Groups | `manage_group`, `manage_group_members` |
| Permissions | `manage_roles`, `manage_tool_policy`, `manage_agent_vault_access`, `decide_approval` |
| Projects | `manage_project`, `manage_project_access` ⚠, `manage_status_models`, `manage_project_sprints` |
| Workspace | `manage_entity_folders`, `manage_workspace_members`, `manage_workspace_settings`, `delete_workspace`, `manage_integrations` |
| Skills | `manage_skills` |
| Squads | `manage_squad` |
| Connections | `manage_connections` |
| Credentials | `manage_credentials` |
| Workflows | `manage_workflows` |
| Channels | `manage_channels` |
| Read access | `read_issues` ⚠, `read_projects` ⚠ |

⚠ = `ManagedExternally: true` (5 total: `autopilot_webhook`, `daemon_runtime_callback`,
`manage_project_access`, `read_issues`, `read_projects`). For these the tool-policy gate is
**not** the enforcement point — they are listed for visibility only and a policy row on them
does nothing. They are a permanent code-only set, not a backlog item.

**Two dimensions, do not confuse them:**
- **Platform capabilities** (this catalog, `manage_*` / `create_*` keys) — coarse HTTP/action
  permissions, keyed on action.
- **Runtime tool capabilities** — the per-tool dimension keyed on `tool_key` values like
  `tools:<Name>` (built-ins), `mcp__<server>__<tool>` (MCP), `connection:<name>`
  (connections). These come from `cerebro_capability` (runtime-reported, registered by
  `runtime_capabilities_cerebro.go`) and the registry in `tools_registry.go`
  (`ToolStatusExcluded` removes ~47). The 5 interfaces resolve both dimensions through the
  same `cerebro_tool_policy` table; the catalog above is only the platform-action half.

---

## 3. The four parallel models (the honest reality)

| # | Model | Backing | Authored where | In the 5 interfaces? |
|---|---|---|---|---|
| **M1** | **HTTP role / membership / ownership middleware** | hardcoded Go string compares + membership rows | nowhere (code) | **No** |
| **M2** | **Tool-policy chain** (section 2) | `cerebro_tool_policy` | the 5 UI interfaces | **Yes** (this is the slice) |
| **M3** | **Group capabilities** | `cerebro_group_capability` / `_runtime_access` / `_agent_access` | Groups UI + `multica group capability` CLI | partly (group layer only) |
| **M4** | **Per-feature stores + the grant resolver** | one bespoke table/config per feature (see 4.3) | a different screen/dialog per feature, or nothing | **No** |

M1 is by far the largest. M4 is the most scattered. M2 is the target; M3/M4 are what we
migrate onto it.

---

## 4. Completeness register — every code-only gate (NOT in the 5 interfaces)

This is the deliverable: every access decision that the 5 permission interfaces do **not**
surface or control. Grouped by model. **Default** = behavior with no configuration.

### 4.1 M1 — HTTP role / membership / ownership (code-only)

Middleware defs in `server/internal/middleware/`; helpers `roleAllowed` (`handler.go:744`),
`requireWorkspaceRole` (`handler.go:798`), `isWorkspaceAdmin` (`access.go:30`).

| Gate | Where | Protects | Default |
|---|---|---|---|
| `RequireWorkspaceRoleFromURL("owner","admin")` | `middleware/workspace.go:185` | workspace admin subtree: UpdateWorkspace, members CRUD, invitations, **tool-policy write**, web-fetch-policy write, agentvault write, approvals decide, workspace-copy, github/lark connect | hardcoded role |
| `RequireWorkspaceRoleFromURL("owner")` | `workspace.go:185` | `DELETE /api/workspaces/{id}` | hardcoded role |
| `RequireWorkspaceRole("owner","admin")` | `workspace.go:167` | group writes, role writes, auth-settings, status-model writes | hardcoded role |
| `RequireWorkspaceMember` / `…FromURL` | `workspace.go:161/173` | the entire workspace-scoped API subtree | membership row |
| `RequireUserScope` | `middleware/scope.go:85` | reject task-token actors | scope flag |
| `AllowTaskScopeForIssue/Agent/Workspace` | `scope.go:98/116/137` | bind a task token to its issue/agent/workspace | token binding |
| `RequireHumanActor` | `handler/actor_guards.go:96` | `/api/cloud-billing/*` (block machine actors) | `X-Actor-Source` header |
| ~40 in-handler `roleAllowed` / `requireWorkspaceRole(...,"owner","admin")` | agent.go, squad.go, project_access.go, skill.go, runtime.go, runtime_tools_admin_cerebro.go, artifact.go, comment.go, … | per-action owner/admin gates and author-or-admin ownership compares | hardcoded role / ownership |
| `requireChannelMember` (+ `channels/permissions.go` `CanRename/ManageMembers/SelfLeave`) | `channels/handler.go:435`, `permissions.go:127-157` | every channel/DM action; per-channel rename/member-mgmt | subscriber row + `channel_permissions` table; **agent caller always privileged**; read-err **fail-open** |
| last-owner guards | `workspace.go` (`countOwners<=1`), `invitation.go:82` | block removing/demoting the sole owner; reject `owner` invites | hardcoded |
| signup gate | `handler/auth.go:235` `checkSignupAllowed` | who may sign up | env/config |

### 4.2 M3 — Group capabilities (separate store, partly visible)

| Gate | Where | Protects | Default |
|---|---|---|---|
| `CanCreateRuntime` / `CanCreateAgent` | `grouppermissions/permissions.go:295,300` | create a runtime / agent | admin→true; else `cerebro_group_capability` row |
| `CanUseRuntime` / `CanUseAgent` | `permissions.go:322,336` | use a runtime / trigger an agent | admin→true; else `_runtime_access` / `_agent_access` rows |
| `CanSeeProjectViaGroup` | `permissions.go:352` | project visibility via group | DB rows |

*(These are settable via the Groups UI + `multica group capability`, but they are a
**different store** from `cerebro_tool_policy` and only partly overlap the 5 interfaces —
there is no workspace/runtime/agent/user authoring of these capabilities, only group.)*

### 4.3 M4 — Per-feature stores + the grant resolver (code-only, scattered)

| Gate | Where | Protects | Default | Backed by |
|---|---|---|---|---|
| **Credentials** `enforce` | `credentials/service.go:95`; chain `cerebro_credentials_policy.go:248` | attach/read/reveal/rotate/revoke a secret | **deny-by-default for agents** | the **grant resolver** (`permissions/resolver.go`, now a deny-by-default floor — `cerebro_workspace_grant` dropped, FIR-1512) + owner check + tool-policy chain |
| **web_fetch host policy** | `webfetchpolicy/policy.go:73,156`; gate `firtal_gateway_tools_extended.go:830` | which hosts `web_fetch` reaches | allow-list `{firtal.com, docs.anthropic.com}` | `cerebro_web_fetch_policy` table |
| **firtal_registry scope** | `firtal_gateway_tools_extended.go:630,545` `loadGrantConfig` | data-source/app/write scope | **deny-by-default** (`allow_write` not implied by read) | per-agent `agent_tool_grant.config_json` |
| **agentvault** | `agentvault/resolver.go:50` `AllowedVaults` | which secret boxes the agent token is scoped to | empty → no brokering | per-agent `Access[]` list; flag `cerebro_agent_vault` (off) |
| **autopilot scope** | `access/autopilot_scope.go:118,150,179` `CanSee/Edit/Trigger` | autopilot visibility/edit/trigger | unknown scope → **fail closed**; private → creator-only | `autopilot.scope` columns |
| **sandbox profile** (OS wall) | `sandboxprofile/profile.go:91-139`; `daemon/sandbox.go:98` | network mode, writable/denied paths, shell deny, keychain | empty → Developer (open); ReadOnly → DenyShell; keychain **deny-by-default** for new agents | hardcoded preset → `sandbox_policy` jsonb |
| **commentguard** | `commentguard/guard.go` | reject agent comment with no recipient + sub-issue rules | **flags default OFF**; DB err → fail-open | feature flags |
| **tool exposure / grant-allowlist** | `tools_registry.go:122,146` `GetCascadeEnabledToolsForAgent` / `ResolveCerebroAgentToolAccess` | which tools an agent is handed (empty → chat-only) | unconfigured runtime → **falls back to legacy `agent_tool_grant`** (fail-open, not fail-closed) | `cerebro_runtime_tool*` tables (a **different** table family from `cerebro_tool_policy`) |
| **tool registry exclusion** | `tools_registry.go:308` `ToolStatusExcluded` | ~47 tools never registered | code-owned | source code |
| **private-agent access** | `handler/agent_access.go:75` | who sees/uses a private agent | creator/owner-only | agent row |
| **folder action guard** | `handler/artifact_folder_action_guard_cerebro.go` `requireFolderActionAllowed`; calls in `artifact_folder.go` delete/update | who may delete/rename/move a folder | **flag default OFF** (any member); when ON for the folder's OWNER → owner + workspace owner/admin only; unowned → open; lookup err → fail-open | flag `cerebro_folder_action_guard` resolved for the folder owner (locked-ws > owner personal > unlocked-ws > default) |
| **daemon repo allowlist** | `daemon/daemon.go:885` `workspaceRepoAllowed` | repo URL in the daemon's in-memory allowlist | not present → false | in-memory list |
| **autopilot webhook scope** | `handler/autopilot_webhook.go:663` | autopilot webhook event filter | missing → **fail closed** | trigger config |
| **wakeup self-limits** | `wakeup/service.go:465` | max wakeups/issue + min interval | limit-based (anti-flood, not access) | workspace settings |
| **the capability engine** (grant resolver + permgate, FIR-2193) — **documented in §5.2** | `permissions/resolver.go` `Can` + `permgate/permgate.go` | credentials + repo grants + the approval inbox | **deny-by-default** (resolves against an empty grant set → Deny) | no table — `cerebro_workspace_grant` dropped (FIR-1512 Step A, see §5.2) |

### 4.4 Not access control — do NOT fold these into the model

`budget` (`service/budget.go` spend cap), `ratelimit`, `agentpass` scope
(`agentpass/scope.go` — a pass may only *narrow*, not a tool gate), provider auto-pause /
codexlimit, `permguard` (test-time inventory), Cloudflare Access (authentication, not policy).

---

## 5. The capability engine (grant resolver + permgate) — a usable function, and Persona's status

The word "grant" covers two different things here. Keep them separate: the **external Persona
service** (gone) and the **capability engine** (a live, usable function — documented below, NOT
cruft to delete ad-hoc).

### 5.1 Persona — the external service + SDK — REMOVED ✅ (FIR-1497 / FIR-1609 Phase 8 / FIR-1777)
Persona-the-service was gated entirely behind `MULTICA_PERSONA_URL`/`_TOKEN`, **unset in prod**
(verified across all Infisical folders), so removal was zero prod-behavior risk. Removed:
- [x] `CutoverPolicyChecker` / `NewPersonaPolicyChecker` / `PersonaChecker` / `personaBackend` and the
  `MULTICA_PERMISSION_ENGINE` / `_PARALLEL_SAMPLE` switch — `newCredentialsPolicy` returns
  `ChainPolicyChecker(owner, multica)` unconditionally; `multicaCredentialPolicy` is the sole checker.
- [x] `cerebro-persona-hook` binary + the dev-only `multica agent e2e-spawn` command.
- [x] persona-mask (`cerebro_persona_mask.go`, `internal/cerebro/persona/mask/`, the 10 `handler/persona_mask_*` files + tests, migration 9021).
- [x] daemon spawn persona path (`daemon/persona.go`, `persona_http.go`, config persona fields).
- [x] share-token feature (`internal/cerebro/sharetoken/`) — Persona was its only auth; public-share retired (Jesper: "bruger vi ikke").
- [x] persona handlers + routes (`handler/persona.go`, `persona_approvals.go`, `ListWorkspacePersonaAgents`, `UpdateAgentRuntimePersonaSandbox` + all `/api/persona/*` + `/persona-sandbox` route mounts).
- [x] SDK package `packages/cerebro-persona-sdk/` + `server/go.mod` `replace`/`require` + persona dev scripts.
- [x] **Agent-facing grant surfaces (FIR-1777, PR #1688):** the `get_grant` runtime tool, the
  `list_grants`/`create_grant`/… MCP tools, and the `multica grant` CLI. An agent can no longer
  see or call grants, and nothing advertises "Persona" to a runtime. The prior "keep Persona as a
  separate service" plans are archived under [`docs/archive/persona/`](../archive/persona/) — do not act on them.

Still inert and tracked for a later sweep: the `agent.persona_sandbox` / `agent_runtime.persona_sandbox`
DB columns + the frontend persona-sandbox tab. The old persona Go package was renamed to `spawn`,
reduced to `server/internal/cerebro/spawn/spawn.go` (`ResolveSpawnSubject`, used by the daemon — keep).

### 5.2 The capability engine — deny-by-default floor + approval seam (engine-flip Step A done)
`permissions.Resolver` (`permissions/resolver.go`, FIR-2193) + `permgate`
(`permgate/permgate.go`) are the **deny-by-default security floor + approval engine**. As of the
FIR-1512 engine-flip Step A the resolver no longer reads any grant table: every grant-authoring
surface was removed (Spor 1 #1688, #1768), the `cerebro_workspace_grant` table is dropped
(migration `9102_cerebro_drop_workspace_grant`), and `Can` resolves against an **empty grant
set**. Use it as follows:

- **What it answers:** `Can(actor, capability, resource) → Deny`, always — **deny-by-default**
  with no grant able to exist (`capability == ""` returns the capability-required deny). This is
  byte-for-byte the verdict the prior algorithm produced against the already-empty table; only
  the synchronous table read (and the table) are gone. The grant-evaluation machinery
  (subject layering, `capabilityMatches`, `resourceMatches`, time-windows) was removed with it —
  it only ever processed grant rows that can no longer exist.
- **It is the FLOOR, not the allow source.** Each call site supplies its own allow authority on
  top of this deny floor: credentials add an upstream **owner-allow** check (`ChainPolicyChecker(owner, multica)`)
  and the tighten-only tool-policy chain; the approval gate uses the tool-policy chain via
  `EvaluateDecision`. Removing the floor itself (engine-flip option B) is a separate, larger
  decision and is intentionally NOT part of Step A.
- **permgate** is the enforcement seam that turns a `NeedsApproval` verdict into a real entry in
  the approval inbox and blocks the action until a human approves / rejects / it expires
  (cross-process, by polling the approval row). Consulted by: the credentials policy, the daemon
  tool-policy gate, repo-approval, the runtime approval gate, and the firtal-gateway executor.
- **Where it is wired live today:**
  - **Credentials** — the deny-by-default secret gate (`cerebro_credentials_policy.go`): owner
    passes; an agent needs a grant. The unified tool-policy chain is layered on top as (a) a
    tighten-only **cap** and (b) — flag-gated `cerebro_credential_chain_grant` (default OFF) —
    an **explicit-Allow grant source** (FIR-1609 Phase 7 keystone, `chainCredentialSignal` →
    `foldCredentialVerdict`). The keystone is the safe step that lets the chain eventually own
    credential grants: it grants only from an authored Allow row (`Effective.DecidedBy != ""`),
    never a no-row Base=Allow default, so it can never widen who reveals a secret by default.
  - **Repo grants** — `repo.checkout/read/push`, but only when the `repo_grants_enabled`
    workspace setting is ON. Default OFF ⇒ repo checkout is `Base=Allow` via the tool-policy
    chain (`CheckDaemonRepoCapability`), **not** this engine.
- **Current data state (Firtal):** the `cerebro_workspace_grant` table is **dropped** (FIR-1512
  Step A). It previously held only inert `workspace_default` rows and **zero** credential/repo
  grants. Secrets live in **Infisical + agent vault**, and credentials are **owner-only** in
  practice. The engine grants nothing beyond owner access; the resolver remains as the
  deny-by-default floor the call sites layer their allow authority on (see §5.3).

### 5.3 Consolidation path — via the engine-flip (FIR-1512), never a blind drop
The end state moves credential (and other) grant authority off `cerebro_workspace_grant` and
onto the unified tool-policy chain, then retires the table. That is **FIR-1512** ("Engine-flip —
pensionér grants") + FIR-1739 — a deliberate change, **not** a one-shot delete. Because the
resolver + permgate are wired into the live credential / approval / repo paths, an unsequenced
`DROP` breaks those gates. Safe sequence:
1. Credential keystone on the chain — **done** (flag-gated, §5.2).
2. Migrate any real grants → tool-policy Allow rows (fail-closed if not 1:1), then flip the
   credential flag on and retire the deny-by-default floor.
3. Repoint repo + workspace-copy off the table; remove the operator grants surfaces.
   - Repo capability — **done** (FIR-2505): `CheckDaemonRepoCapability` resolves through the
     tool-policy chain, not the grant table.
   - Operator grants UI (web/desktop) — **done** (FIR-2284): no remaining frontend caller.
   - workspace-copy (`workspacecopy/copy_roles.go`) — **done** (FIR-1777): the foundation +
     relink passes no longer copy `cerebro_workspace_grant`; roles/groups/capabilities still copy.
   - `cerebro/grants` server handler + `router.go` `/grants` routes + the `cerebro_grants` /
     `cerebro_persona_permissions` settings flags — **done** (FIR-1777): the operator grant
     control plane is fully removed (handler package deleted, routes unmounted, `manage_grants`
     dropped from the platform catalog + permguard inventory).
   - Reader decoupling — **done** (FIR-1512 Step A): the credentials resolver and permgate no
     longer read the table; `Can` resolves against an empty grant set (deny-by-default).
4. Drop `cerebro_workspace_grant` (+ `_audit`) and reduce the resolver — **Step A built**
   (migration `9102_cerebro_drop_workspace_grant`, grant-evaluation machinery removed,
   `approval_required` / `time_window_*` / `classification_ceiling` gone with the table since
   they carried no live enforcement). The migration applies to **staging** with `main`; the
   **prod DROP is irreversible and gated on explicit approval** — it does not auto-promote.

### 5.4 Credentials are ONE permission type — and the parallel store NOT to wire in

A credential follows the same rule as everything else: **the permission to use a credential is a
permission type in the unified tool-policy chain, and nowhere else.** There is exactly one place a
credential *permission* is authored and enforced. Do not add a second — and the section below names
the specific second model you will find in the code and must NOT cement.

- **The credential permission type (the one true model).** A credential permission is a
  `cerebro_tool_policy` row like any other tool: ToolKey
  `credential.<attach|read_redacted|reveal|rotate|revoke>`, ResourcePattern
  `cerebro-credential:<uuid>` (id scope, most specific) or `cerebro-credential-type:<type>` (type
  fallback) — the convention pinned in `cerebro_credentials_policy.go` (`multicaCredentialPolicy.Check`,
  FIR-1609 Phase 7). The chain is **already** consulted there: always-on as a tighten-only **cap**,
  and — flag `cerebro_credential_chain_grant` (default OFF) — as an **explicit-Allow grant source**.
  This is the model credentials converge on. The deny-by-default floor (`cerebro_workspace_grant`,
  §5.2–5.3) is the *retiring* step underneath it, not a second model.

- **The secret itself lives in Agent Vault — that is storage, not a permission model.**
  `cerebro_agentvault_agent_access` (TECH-3196, flag `cerebro_agent_vault`) is *where credentials
  live*: at task claim the server reconciles this table from the authoritative tool-policy grants
  (FIR-1739 Part B), so it is a derived view of the chain, not an authority. **FIR-2210:** the old
  agent-side forward-proxy transport (claim-time `HTTPS_PROXY`/CA injection + the standalone relay
  listener) was removed — agents now reach a credential through Multica connections (the FIR-2166
  server-side connection proxy), never a tunnel and never the internal Agent Vault path directly. The
  table is storage + the per-agent grant model the connections path consumes, **not** a second place
  to express "who may use this credential" — do not confuse the vault-access table for a permission
  interface.

- **The `cerebro_credential_policy` parallel store is RETIRED (FIR-1479) — there is no second model
  to wire in.** Earlier the `credentialpolicy` package + `cerebro_credential_policy` table (migration
  9096), handler `agent_credential_grant_cerebro.go`, endpoint `/api/agents/{id}/credential-grants`,
  and `AgentCredentialGrantsPanel` backed a per-actor credentials column as a SECOND, separate
  authoring/display model. `multicaCredentialPolicy.Check` never read it. That parallel path is now
  removed: the per-actor credentials authoring surface lives on the tool-policy chain credential rows
  above (`toolpolicy/table_credential.go`, flag `cerebro_credentials_per_actor` for visibility,
  writes gated by `manage_credential_access`), so authoring and enforcement read the SAME store. The
  migration-9096 table is left orphaned (non-destructive), not dropped. If you are tempted to add a
  new credentials-specific authoring store or to read a display-only column as an "enforcement gap to
  be wired in," don't — that was the mistake this section was written for (it cost a review cycle on
  FIR-1739). Author credential access as `credential.*` rows on the chain, nowhere else.

---

## 6. Target architecture — converge on the 5 interfaces

The end state we are building toward (FIR-1496):

1. **One read model.** Extend `toolpolicy/table.go` so a single per-tool view reports **every**
   layer that can block — M1 (role/membership), M2 (tool-policy), M3 (group caps), M4
   (per-feature guards + exclusion + exposure) — each with `blocked_reason` + `how_to_fix` +
   `who_can_fix`. Today only M2's group-cap blame (`CappedByGroups`) is surfaced.
2. **One authoring surface.** Every gate in section 4 that *should* be operator-controllable
   gets a row in one of the 5 interfaces. Gates that must stay code-owned (registry exclusion,
   last-owner guard, signup) are documented as such so "code-only" reads as a deliberate choice,
   not a missing feature.
3. **Credentials onto the chain** (Base=Deny), retiring the grant resolver for credentials. The
   separate `cerebro_credential_policy` authoring store (FIR-1479) has folded onto the chain
   credential rows and is now retired (§5.4) — it was never wired into `Check` as a parallel
   enforcement path.
4. **Persona service removed** (§5.1, done); the **capability engine** (§5.2) is a documented,
   usable function that consolidates onto the chain via the engine-flip (FIR-1512, §5.3) — never
   an ad-hoc drop.
5. **Visible enforcement state.** The flags that decide whether policy rows are actually
   enforced (`cerebro_local_tool_policy(_enforce)`, `cerebro_approval_gate`) must be shown next
   to the policy tables — an admin setting Deny must see whether enforcement is on.

Until then: **do not assume the 5 interfaces are the whole story.** Before claiming "X is
ungated", find the runtime call site — it is very likely one of the section-4 code-only gates.
