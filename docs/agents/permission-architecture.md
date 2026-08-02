# Permission architecture — the real structure & completeness register

> **FIR-3402 canonical access source.** A direct layered policy decision and a
> reusable versioned Role are two authoring forms for the same permission
> contract. `toolpolicy.Store` expands active Role bindings into the same Policy
> Decision Service input used by direct policy rows; expired or archived
> bindings are filtered with the database clock on every resolve, and the
> effective explanation records the deciding Role name and version.
> A per-run Task Mandate then freezes the allowed-tool envelope. The immutable
> snapshot is always captured and remains observable, but it is enforced again
> on managed and local-runtime calls only while
> `cerebro_task_mandate_enforcement` is on. Migration 9147 deletes the two
> superseded direct stores; automated contract coverage prevents either route
> or SQL access path from returning.

> **FIR-3403 runtime-tool consolidation.** The capability register is the sole
> runtime-tool inventory, availability evidence proves discovery, and
> `cerebro_tool_policy` is the sole access authoring store. Migration 9148 moves
> existing runtime/group/user/agent choices before irreversibly deleting the
> former runtime-tool tables. A source regression test rejects any new Go or SQL
> reader for those retired tables.
> Role expansion is constructor-independent: generated query sets expose their
> original DB adapter to `toolpolicy.Store`, and a missing role store fails the
> resolve instead of omitting roles. The server-owned governance sweeper reads
> live role assignments on startup and daily, expires only the affected role
> assignments through a monotonic tighten-only action, and reports expired,
> orphaned, and unused access by severity. The Permission
> audit read model exposes and sorts by the same explicit severity concept.

> **FIR-4012 system-authored capability rules.** The `driftwatch` sweeper is a
> second, non-human writer into `cerebro_tool_policy`. When
> `cerebro_capability_auto_permission` is ON (default OFF) it calls `Store.Set`
> at `LayerAgent` for every capability an agent used that has NO policy row,
> with `Setting=allow` and a zero `UpdatedBy` — which `Store.recordAudit` stamps
> as a `system` write, so these rows are distinguishable from a human choice in
> the change log. Two invariants hold and must keep holding: it only ever writes
> where no row exists (a `deny` row is never overwritten, so the sweeper cannot
> loosen a deliberate choice), and it never writes `deny` (every capability in
> its input is one the agent already used, so an auto-deny would break working
> agents overnight). Its purpose is to make an ungoverned capability appear in
> the permission table at all; the decision to block stays human.

**Read this together with [`permission-system.md`](./permission-system.md).** That doc is the
*behavioral* map ("what is enforced live and which safety intersections apply", row by row). **This**
doc is the *structural* map: what permission **models** actually exist, which gates are
authored through the 5 permission interfaces vs. which live **only in code**, and the
decision to remove Persona. If you change the permission model itself (add an interface,
move a gate into the tool-policy chain, remove Persona), update this file in the same PR.

Line numbers drift; this doc cites files + functions. Grep the function name if a line moved.

> **Secure browser fill (FIR-3006):** this is a code-only compound gate, not a
> new permission model. It requires `tools:personal-browser` for the target host
> and an explicit exact-resource `credential.reveal` Allow authored on the
> `browser-testers` group. The owner must be a member of that group and the
> exact calling agent must be on the same group's agent allowlist. The resource
> is one app-specific Agent Vault box,
> `agentvault-vault:Shared/browser-login/<app>`; no capability-wide reveal is
> accepted. Plaintext exists only between the backend Agent Vault client and the
> desktop Chromium injection frame.

---

## 1. The mistake this doc exists to prevent

Multica has **one authored permission model**: the unified tool-policy chain
(Allow / Ask / Deny / Inherit across **Workspace › Runtime › Agent › Group › User**),
with Roles expanded into the Agent layer. It is intersected by code-owned safety
ceilings for credentials, sandboxing, repository access, ownership and other
non-delegable invariants. Those ceilings are not alternative places to author
the same permission; they can only preserve or tighten the canonical verdict.
Section 4 is the register of those deliberate intersections and of remaining
legacy visibility/access models.

---

## 2. The canonical model — capability register + tool-policy layers

The capability register says what exists; the unified tool-policy chain says
who may use it. Runtime discovery writes capability evidence, never access.
Workspace/runtime/agent/group/user choices are authored only in
`cerebro_tool_policy`; runtime executors, the Permissions UI,
`get_agent_capabilities`, and the injected tool brief resolve that same source.

- **The 5 interfaces = the 5 layers** of the chain: `workspace / runtime / agent / group / user`
  (`server/internal/cerebro/toolpolicy/chain.go:62-70`). These are the 5 settable surfaces
  the UI renders (`packages/cerebro-tool-policy` `ToolPolicyTable`, `view` prop →
  `VIEW_EDIT_LAYER`).
- **Roles are reusable authoring packages, not a sixth layer.** Active Role
  rules are expanded into the Agent layer for capability-wide and exact-resource
  decisions alike (repositories, credentials and Connection tools/endpoints).
  Direct Agent rules remain a tighten-only ceiling over a Role, and the
  explanation carries Role name + version provenance.
- **Task Mandate is a run snapshot, not a second policy store.** It freezes the
  exact allowed-tool envelope when a task is claimed. The task transcript and
  `multica permissions task <id>` expose that historical snapshot.
  `cerebro_task_mandate_enforcement` controls whether managed and local runtimes
  apply it as a call-time ceiling; turning that flag off does not change the
  snapshot or bypass tool policy, credential, sandbox, repository, ownership,
  approval, or other independent safety floors.
- **Resolver:** `toolpolicy.ResolveWithMode` contains the two pure folds. Public
  DB callers use `Store.ResolveDeclared` or `Store.ResolvePermission`, so the
  key registry — not the caller — selects hard-floor versus openable semantics.
  `Inherit`/absent is pass-through and the base default is Allow.
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
- **Member-override mode (FIR-2175/FIR-3062, flag `cerebro_member_override`, default ON):**
  `ModeOpenable` is a two-stage variant — Stage A
  resolves the human layers `Workspace › Group › User` by **specificity** (most specific wins,
  so a member's own Allow can OPEN what their group denied — it can LOOSEN, not only tighten),
  Stage B lets `Runtime`/`Agent`/`on_behalf_of`/`System` only tighten that member ceiling
  (`on_behalf_of` — the delegated task initiator, FIR-2441 — tightens identically
  under both modes). `Store.ResolveDeclared` reads the workspace flag and applies
  `DeclaredResolutionMode`; hard-floor key classes always select `ModeHardFloor`
  even when the flag is on.
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
  the declared `human_opt_in_or_admin` contract through `ResolvePermission` — **OFF by default
  for non-admin members, opt-in only**; Base=Allow would be backwards here since
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
- **CLI surface (FIR-1609/FIR-3388):** `multica permissions explain|set|clear`,
  `multica permissions roles list|show|create|update|archive|assign|unassign`,
  and `multica permissions task <id>`
  (`server/cmd/multica/cerebro_permissions.go`) wraps the read model + write surface above —
  `explain` (GET) prints per-tool `Effective` + `DecidedBy`/`CappedBy`/reason + group blame to
  answer "why is this agent+member blocked"; `set`/`clear` (PUT/DELETE) author/remove one
  Allow/Ask/Deny rule at one layer, optional WHEN/CEL condition; `roles` manages reusable
  versioned packages and bindings; `task` prints the immutable run snapshot. The server
  keeps the same ownership/admin gates as the matching UI operations.

**What actually routes through these interfaces today (Class A — the whole list):**

| Gate | Where | Live? |
|---|---|---|
| agent-browser unix-socket (`tools:agent-browser`, **Base=Deny**) | `daemon_tool_policy_cerebro.go:281` | live |
| personal browser per-action host gate (`tools:personal-browser`, agent opt-in + feature kill switch) | `personal_browser_authorize_cerebro.go` `AuthorizePersonalBrowser` | live |
| agent-browser Agent Vault login provisioning (`credential.reveal`, exact `Shared/browser-login/<app>` box + owner membership and exact-agent allowlist in `browser-testers`) | `handler/agent_browser_auth_cerebro.go` | live |
| repo checkout (`repo.checkout`, Base=Allow) | `handler/repo_approval_cerebro.go:42` `CheckDaemonRepoCapability` | live |
| `create_local_runtime` | `group_permissions_cerebro.go:186` | live |
| `manage_connections` | `group_permissions_cerebro.go:237` (+ router middleware) | live |
| `create_issue` platform action | `platformaction/gate.go`; REST hook `handler/issue.go`; workspace MCP hook `handler/workspace_mcp_cerebro.go`; Gateway hook `runtime/approval_gate.go` | live, always-on for agents |
| connection per-tool Deny/Ask | `table_connection.go:305` `ConnectionToolEffective` | live |
| mention `trigger_other_agent` (layered over a code baseline) | `mentiongate/gate.go:92` | live |
| general gateway tool calls (Policy Decision Service via `accessdecision.Service`) | `runtime/access_decision.go` + `approval_gate.go` | **live and fail-closed**; no server rollout switch |
| local-CLI tool calls (Claude/Codex/Cursor/Gemini, via `ResolveDeclared`) | `daemon_tool_policy_cerebro.go:68` | **always enforced**; provider adapters fail closed, and local CLI providers without an adapter are rejected before spawn |

Both **general** gates resolve through `Store.ResolveDeclared`. When
`cerebro_member_override` is on, ordinary keys use the openable mode; declared
floor keys remain hard-floor and are not affected by that flag.

Everything below in section 4 is **not** on this list — it is a separate model.

---

## 2a. The canonical platform-action inventory — 62 capabilities

There is already an authoritative, code-owned inventory of platform actions:
`server/internal/cerebro/platformcatalog/catalog.go` (`var catalog`, exposed via `All()`).
It was built from the hardcoded-policy audit (Jesper, 2026-05-31) **specifically so the
buried code gates become settable instead of invisible**. Each entry carries: `Key` (the
`tool_key` an `cerebro_tool_policy` row binds to), `Title`, `Category`, `Description`, `Ops`
(the real HTTP routes it covers), and/or `Evidence` (file:line of the hardcoded check when
it is not a single route). A capability MUST have at least one of `Ops` or `Evidence` — that
is the traceability tripwire.

**This catalog is the canonical scope of platform actions.** Count `catalog` directly in
`platformcatalog/catalog.go`; the table below lists a representative subset, not every key.
The Settings API always surfaces the complete catalog when Unified tool permissions is active.
Engine-owned entries run through the canonical policy gate. Entries governed elsewhere are
read-only and must name their concrete `ExternalSecurityOwner`.

**Focused agent-start surface (FIR-3091 slice 4).** The four "start someone else's agent"
capabilities — `trigger_other_agent`, `rerun_issue`, `schedule_agent_wakeup`, `trigger_autopilot`
(marked `Surfaced` in the catalog, see `platformcatalog.SurfacedKeys`) can also appear in focused
Agent-start guidance behind `cerebro_agent_trigger_permissions`. That flag controls presentation
only; all four rows remain visible in the canonical Permissions table. `trigger_other_agent` is
engine-owned; the other entries name the external access gate that owns their enforcement.

| Category | Capabilities (`tool_key`) |
|---|---|
| Issues | `create_issue`, `update_issue`, `delete_issue`, `rerun_issue`, `subscribe_issue`, `manage_labels`, `manage_issue_recurrence` |
| Comments | `add_comment`, `update_comment`, `manage_sessions` |
| Autopilots | `create_autopilot`, `trigger_autopilot`, `autopilot_scope`, `autopilot_webhook` ⚠ |
| Artifacts | `manage_artifacts`, `manage_notes`, `manage_note_types` |
| Agents | `create_agent`, `update_agent`, `trigger_other_agent`, `schedule_agent_wakeup`, `manage_agent_passes`, `manage_work_sessions`, `create_memory` |
| Runtimes | `manage_runtime`, `manage_runtime_accounts`, `manage_cloud_runtime`, `create_runtime`, `create_local_runtime`, `use_other_runtime`, `daemon_runtime_callback` ⚠ |
| Groups | `manage_group`, `manage_group_members` |
| Permissions | `manage_roles`, `manage_tool_policy`, `manage_agent_vault_access`, `decide_approval`, `manage_credential_access`, `manage_group_overrides`, `manage_workspace_overrides`, `manage_collections` |
| Projects | `manage_project`, `manage_project_access` ⚠, `manage_status_models`, `manage_project_sprints` |
| Workspace | `manage_entity_folders`, `manage_workspace_members`, `manage_workspace_settings`, `delete_workspace`, `manage_integrations`, `manage_analytics`, `manage_model_registry` |
| Skills | `manage_skills` |
| Squads | `manage_squad` |
| Connections | `manage_connections` |
| Credentials | `manage_credentials` |
| Workflows | `manage_workflows`, `hooks:read`, `hooks:write`, `hooks:enforce`, `hooks:manage_managed` |
| Channels | `manage_channels`, `gateway_channel_delivery` ⚠ |
| Read access | `read_issues` ⚠, `read_projects` ⚠ |

(`manage_share_tokens` was removed with the share-token feature and no longer exists; drop it if
you find it cited elsewhere.)

⚠ = `ManagedExternally: true` (6 total: `autopilot_webhook`, `daemon_runtime_callback`,
`manage_project_access`, `read_issues`, `read_projects`, `gateway_channel_delivery`). For these the tool-policy gate is
**not** the enforcement point — they are listed for visibility only and a policy row on them
does nothing. They are a permanent code-only set, not a backlog item.

**Two dimensions, do not confuse them:**

Workflow Hook capabilities are independent of `manage_workflows`. `platformcatalog` binds each concrete hook tool to one permission key; `platformaccess` owns that key's contract, and `toolpolicy.ResolvePermission` projects it into the capability card, claim-time tool list, and call-time authorizer. Agents receive `hooks:read`, while `hooks:write` needs an explicit grant, `hooks:enforce` rejects agent actors, and `hooks:manage_managed` accepts only the workspace owner. Tool registration therefore means only `available`; it never implies `allowed` or `callable`. The complete eight-key special-contract list lives in `permission-system.md`; this architecture document does not duplicate it.

Workflow, Command, and Eval mutations under `/api/cerebro/workflows*`, `/api/cerebro/commands*`, and `/api/cerebro/evals*` share one agent enforcement boundary: `handler.RequireManageWorkflows` always checks `PlatformActionGate` against `manage_workflows` and also checks Task Mandate while `cerebro_task_mandate_enforcement` is on before the route handler can mutate state. Reads and member behavior remain unchanged, and the independent `/api/cerebro/workflow-hooks*` family keeps its `hooks:*` contracts. Settings → Permissions is therefore the only authoring surface for `manage_workflows`; CLI/MCP wrappers inherit the HTTP decision.

Externally managed catalog rows remain read-only and render `external_security_owner` inline beside `Managed externally` in both permission catalog presentations. An older response without an owner renders `Security owner not specified`, so the management location never depends on hover or disappears silently.
- **Platform capabilities** (this catalog, `manage_*` / `create_*` keys) — coarse HTTP/action
  permissions, keyed on action.
- **Runtime tool capabilities** — the per-tool dimension keyed on `tool_key` values like
  `tools:<Name>` (built-ins), `mcp__<server>__<tool>` (MCP), `connection:<name>`
  (connections). These come from `cerebro_capability` (runtime-reported, registered by
  `runtime_capabilities_cerebro.go`) and the registry in `tools_registry.go`
  (`ToolStatusExcluded` removes 81). The 5 interfaces resolve both dimensions through the
  same `cerebro_tool_policy` table; the catalog above is only the platform-action half.

---

## 3. Remaining permission models around the canonical engine

| # | Model | Backing | Authored where | In the 5 interfaces? |
|---|---|---|---|---|
| **M1** | **HTTP role / membership / ownership middleware** | hardcoded Go string compares + membership rows | nowhere (code) | **No** |
| **M2** | **Capability register + tool-policy chain** (section 2) | `cerebro_capability` inventory/evidence + `cerebro_tool_policy` decisions | Settings → Permissions | **Yes — canonical runtime-tool path** |
| **M3** | **Group capabilities** | `cerebro_group_capability` / `_runtime_access` / `_agent_access` | Groups UI + `multica group capability` CLI | partly (group layer only) |
| **M4** | **Per-feature floors + stores** | one bespoke table/config per feature (see 4.3) | a different screen/dialog per feature, or nothing | **No** |

M1 is by far the largest. M4 is the most scattered. M2 is authoritative for
runtime-tool inventory and policy; M3/M4 remain separate only for the explicitly
listed feature-specific floors and stores.

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
| `AllowTaskScopeForIssue/Agent/Workspace` | `scope.go:98/116/137` | bind a task token to its issue/agent/workspace, including issue-property value writes and workspace property-catalog reads | verified task-token binding |
| `RequireHumanActor` | `handler/actor_guards.go:96` | `/api/cloud-billing/*` (block machine actors) | `X-Actor-Source` header |
| ~40 in-handler `roleAllowed` / `requireWorkspaceRole(...,"owner","admin")` | agent.go, squad.go, project_access.go, skill.go, runtime.go, runtime_tools_admin_cerebro.go, artifact.go, comment.go, … | per-action owner/admin gates and author-or-admin ownership compares | hardcoded role / ownership |
| `sessionmode.Handler.requestContext(write=true)` | `cerebro/sessionmode/handler.go` | Settings-managed Mode drafts, Publish and Restore | workspace owner/admin and human actor only; reads remain member-visible |
| `requireChannelMember` (+ `channels/permissions.go` `CanRename/ManageMembers/SelfLeave`) | `channels/handler.go:435`, `permissions.go:127-157` | every channel/DM action; per-channel rename/member-mgmt | subscriber row + `channel_permissions` table; **agent caller always privileged**; read-err **fail-open** |
| last-owner guards | `workspace.go` (`countOwners<=1`), `invitation.go:82` | block removing/demoting the sole owner; reject `owner` invites | hardcoded |
| signup gate | `handler/auth.go:235` `checkSignupAllowed` | who may sign up | env/config |

### 4.2 M3 — Group capabilities (separate store, partly visible)

| Gate | Where | Protects | Default |
|---|---|---|---|
| `CanCreateRuntime` / `CanCreateAgent` | `grouppermissions/permissions.go:295,300` | create a runtime / agent | admin→true; else `cerebro_group_capability` row |
| `CanUseRuntime` / `CanUseAgent` | `permissions.go:322,336` | use a runtime / trigger an agent | admin→true; else `_runtime_access` / `_agent_access` rows |
| `CanSeeProjectViaGroup` | `permissions.go:352` | project visibility via group | DB rows |
| `apps.RequireCapability` (`apps.create`, `apps.manage`, `apps.delete`) | `cerebro/apps/admin.go` | app creation, lifecycle/Collection management, and deletion | admin→true; else `cerebro_group_capability` row |

*(These are settable via the Groups UI + `multica group capability`, but they are a
**different store** from `cerebro_tool_policy` and only partly overlap the 5 interfaces —
there is no workspace/runtime/agent/user authoring of these capabilities, only group.)*

### 4.3 M4 — Per-feature floors + stores (code-only, scattered)

| Gate | Where | Protects | Default | Backed by |
|---|---|---|---|---|
| **Credentials** `enforce` | `credentials/service.go:95`; chain `cerebro_credentials_policy.go` | attach/read/reveal/rotate/revoke a secret | **deny-by-default for agents** | owner check + direct Deny floor + canonical tool-policy chain; explicit Allow grants remain flag-gated |
| **web_fetch host policy** | `webfetchpolicy/policy.go:73,156`; gate `firtal_gateway_tools_extended.go:830` | which hosts `web_fetch` reaches | allow-list `{firtal.com, docs.anthropic.com}` | `cerebro_web_fetch_policy` table |
| **firtal_registry scope** | `runtime/firtal_gateway_tools_extended.go` + `toolpolicy.chainGateDataSource` | data-source/app/write scope | **deny-by-default** for resource rows; write is never implied by read | canonical `cerebro_tool_policy` resource rows plus server-owned Registry connection configuration |
| **agentvault** | `agentvault/store.go` `ListForAgent`/`SetAccess`, reconciled via `mirror.go` | which secret boxes the agent token is scoped to | empty → no brokering | per-agent `Access[]` list; flag `cerebro_agent_vault` (off) |
| **autopilot scope** | `access/autopilot_scope.go:118,150,179` `CanSee/Edit/Trigger` | autopilot visibility/edit/trigger | unknown scope → **fail closed**; private → creator-only | `autopilot.scope` columns |
| **sandbox profile** (OS wall) | `sandboxprofile/profile.go:91-139`; `daemon/sandbox.go:98` | network mode, writable/denied paths, shell deny, keychain | empty → Developer (open); ReadOnly → DenyShell; keychain **deny-by-default** for new agents | hardcoded preset → `sandbox_policy` jsonb |
| **commentguard** | `commentguard/guard.go` | reject agent comment with no recipient + sub-issue rules | **flags default OFF**; DB err → fail-open | feature flags |
| **Firtal Gateway tool exposure** | `runtime.policyDecisionTools` / `runtime.guardToolCall` | which tools a Gateway agent is handed and may call (empty/error → chat-only) | **fail closed** through the Policy Decision Service; no cascade or `agent_tool_grant` fallback | live per-task registry + canonical capability catalog + `cerebro_tool_policy`; availability-card verification remains reporting-only |
| **task-scoped workflow step capability** | `runtime/firtal_gateway_loop.go` `loopStepCapabilityFromTask`; gateway and external invoke paths in `firtal_gateway_executor.go`, `handler/agent_tools.go`, and `runtime/tool_executor_invoker.go` | lets only the agent task for a steps-enabled workflow block call `open_loop_step` | absent or malformed task context → tool absent; wrong agent/task pair → denied; authored block/phase limit exceeded → durable phase failure | trusted `agent_task_queue.context` + durable loop state; intentionally bypasses ordinary runtime-tool grants only for this exact task capability |
| **tool registry exclusion** | `tools_registry.go:308` `ToolStatusExcluded` | ~47 tools never registered | code-owned | source code |
| **private-agent access** | `handler/agent_access.go:75` | who sees/uses a private agent | creator/owner-only | agent row |
| **folder action guard** | `handler/artifact_folder_action_guard_cerebro.go` `requireFolderActionAllowed`; calls in `artifact_folder.go` delete/update | who may delete/rename/move a folder | **flag default OFF** (any member); when ON for the folder's OWNER → owner + workspace owner/admin only; unowned → open; lookup err → fail-open | flag `cerebro_folder_action_guard` resolved for the folder owner (locked-ws > owner personal > unlocked-ws > default) |
| **daemon repo allowlist** | `daemon/daemon.go:885` `workspaceRepoAllowed` | repo URL in the daemon's in-memory allowlist | not present → false | in-memory list |
| **autopilot webhook scope** | `handler/autopilot_webhook.go:663` | autopilot webhook event filter | missing → **fail closed** | trigger config |
| **wakeup self-limits** | `wakeup/service.go:465` | max wakeups/issue + min interval | limit-based (anti-flood, not access) | workspace settings |
| **approval seam** (`permgate`, FIR-2193) — **documented in §5.2** | `permissions/decision.go` + `permgate/permgate.go` | materialises canonical Ask decisions in the approval inbox | no independent access decision; callers supply the canonical verdict | approval rows + the caller's canonical policy store |

### 4.4 Not access control — do NOT fold these into the model

`budget` (`service/budget.go` spend cap), `ratelimit`, `agentpass` scope
(`agentpass/scope.go` — a pass may only *narrow*, not a tool gate), provider auto-pause /
codexlimit, `permguard` (test-time inventory), Cloudflare Access (authentication, not policy).

---

## 5. The approval seam and retired grant resolver

The word "grant" historically covered the external Persona service, an empty workspace-grant
resolver, and approval-inbox decisions. Persona and the constant resolver are gone; `permgate`
remains as the shared mechanism that materialises a canonical Ask verdict.

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

The inert persona-sandbox columns, API fields, Agent Office fields, and frontend tab were removed in
FIR-3820. The old persona Go package was renamed to `spawn`, reduced to
`server/internal/cerebro/spawn/spawn.go` (`ResolveSpawnSubject`, used by the daemon — keep).

### 5.2 Canonical decisions + approval seam (FIR-3388)

- `permissions/decision.go` defines only the shared decision contract
  (`Allow`, `Deny`, `NeedsApproval`). It does not query a second permission store.
- `permgate` receives a decision already produced by the canonical tool-policy path. It turns
  `NeedsApproval` into an inbox row and waits for approve, reject, or expiry. Allow and Deny pass
  through without another access calculation.
- Credentials preserve the former security posture with a direct Deny floor. Workspace
  owner/admin access is checked first; an authored tool-policy Allow may grant access only when
  `cerebro_credential_chain_grant` is enabled, while Ask/Deny always tighten.
- The old resolver returned Deny for every request after `cerebro_workspace_grant` was dropped.
  FIR-3388 removed it, its dead SQL query, and the duplicate
  `permission_policy_evaluated` activity rows. This does not widen access.
- Repo, Registry, connection, gateway, and daemon gates resolve through their canonical
  tool-policy paths. None calls a fallback grant resolver.

### 5.3 Completed grant-store retirement

FIR-1512 removed the operator grant surfaces and dropped `cerebro_workspace_grant`; FIR-3388
completed the code cleanup by removing the constant resolver and generated query. The remaining
credential floor is deliberate, local to the credential check, and covered by behaviour tests.
Do not recreate a general grant store or add a second decision path beside `cerebro_tool_policy`.

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
  migration-9096 table is left orphaned (non-destructive), not dropped. Its SQL source and generated
  query methods are deleted, and a source guard prevents any Go or SQL reader from returning. If you are tempted to add a
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
3. **Credentials onto the chain** (direct Deny floor), with the grant resolver retired. The
   separate `cerebro_credential_policy` authoring store (FIR-1479) has folded onto the chain
   credential rows and is now retired (§5.4) — it was never wired into `Check` as a parallel
   enforcement path.
4. **Persona service and the constant grant resolver removed** (§5.1-5.3, done);
   `permgate` remains the shared Ask materialisation seam, not a parallel permission engine.
5. **Visible enforcement state.** The settings that affect policy execution (including
   `cerebro_approval_gate`) must be shown next
   to the policy tables — an admin setting Deny must see whether enforcement is on.

Until then: **do not assume the 5 interfaces are the whole story.** Before claiming "X is
ungated", find the runtime call site — it is very likely one of the section-4 code-only gates.
