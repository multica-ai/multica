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
- **Backing table:** `cerebro_tool_policy` — `(workspace_id, tool_key, layer, subject_id,
  resource_pattern, setting)` (migrations `9042` + `9052` workspace layer + `9054`
  resource_pattern). `setting ∈ inherit|allow|ask|deny`.
- **Read model for UI:** `toolpolicy/table.go:101` (`Store.Table`) — one row per capability
  with per-layer settings, `Effective`, and `CappedByGroups` blame.
- **Write surface:** `PUT/DELETE /api/workspaces/{id}/tool-policy` (`toolpolicy/handler.go`).
  *(Note the asymmetry: the gate on this write is the **code-only** `RequireWorkspaceRole(
  "owner","admin")`, section 4 — you must be an admin by a hardcoded role check to author
  policy.)*
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
| general gateway tool calls (`guardToolCallViaPolicy`) | `approval_gate.go:314` | **off by default** (env `CEREBRO_APPROVAL_GATE_ENABLED` + `MODE=toolpolicy`) |
| local-CLI tool calls (Claude/Codex/Cursor/Gemini) | `daemon_tool_policy_cerebro.go:68` | **off by default** (flags `cerebro_local_tool_policy`(+`_enforce`)) |

Everything below in section 4 is **not** on this list — it is a separate model.

---

## 2a. The canonical inventory — the 55 platform capabilities

There is already an authoritative, code-owned inventory of platform actions:
`server/internal/cerebro/platformcatalog/catalog.go` (`var catalog`, exposed via `All()`).
It was built from the hardcoded-policy audit (Jesper, 2026-05-31) **specifically so the
buried code gates become settable instead of invisible**. Each entry carries: `Key` (the
`tool_key` an `cerebro_tool_policy` row binds to), `Title`, `Category`, `Description`, `Ops`
(the real HTTP routes it covers), and/or `Evidence` (file:line of the hardcoded check when
it is not a single route). A capability MUST have at least one of `Ops` or `Evidence` — that
is the traceability tripwire.

**This catalog is the canonical scope of platform actions.** It is **55 capabilities** in 17
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
| Permissions | `manage_grants`, `manage_roles`, `manage_tool_policy`, `manage_agent_vault_access`, `decide_approval` |
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
| **Credentials** `enforce` | `credentials/service.go:95`; chain `cerebro_credentials_policy.go:248` | attach/read/reveal/rotate/revoke a secret | **deny-by-default for agents** | the **grant resolver** (`permissions/resolver.go` → `cerebro_workspace_grant`) + owner check + (dying) Persona |
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
| **the grant resolver** (FIR-2193 capability engine) | `permissions/resolver.go:115` `Resolve` | credentials + grant-preview + repo grants | **deny-by-default** (no grant → Deny) | `cerebro_workspace_grant` (the Persona grant table) |

### 4.4 Not access control — do NOT fold these into the model

`budget` (`service/budget.go` spend cap), `ratelimit`, `agentpass` scope
(`agentpass/scope.go` — a pass may only *narrow*, not a tool gate), provider auto-pause /
codexlimit, `permguard` (test-time inventory), Cloudflare Access (authentication, not policy).

---

## 5. Persona — DECISION: remove it entirely

**Decision (Jesper, 2026-06-18, FIR-1497):** Persona dies and is removed from all of
Multica; the grant store is "dead for now"; everything converges on the tool-policy chain.
This **supersedes** the prior "keep persona as a separate service" plans, now archived under
[`docs/archive/persona/`](../archive/persona/) (`persona-deferred-work.md`,
`persona-finish-plan.md`, `persona-next-session.md`) — do not act on them.

"Persona" is **four different things** with different fates. Do not delete them as one blob.

### 5.1 Delete now — the external Persona service + SDK — ✅ DONE (FIR-1609 Phase 8, branch `feat/fir-1609-permission-engine`)
Persona was gated entirely behind `MULTICA_PERSONA_URL`/`_TOKEN`, **unset in prod** (verified
across all Infisical folders), so removal was zero prod-behavior risk. All of the following are
removed; `go build ./...` + `go vet` green and no `.go` source imports the persona SDK:
- [x] `CutoverPolicyChecker` / `NewPersonaPolicyChecker` / `PersonaChecker` / `personaBackend` and the
  `MULTICA_PERMISSION_ENGINE` / `_PARALLEL_SAMPLE` switch — `newCredentialsPolicy` now returns
  `ChainPolicyChecker(owner, multica)` unconditionally; `multicaCredentialPolicy` is the sole checker.
- [x] `cerebro-persona-hook` binary + the dev-only `multica agent e2e-spawn` command.
- [x] persona-mask (`cerebro_persona_mask.go`, `internal/cerebro/persona/mask/`, the 10 `handler/persona_mask_*` files + tests, migration 9021).
- [x] daemon spawn persona path (`daemon/persona.go`, `persona_http.go`, config persona fields).
- [x] share-token feature (`internal/cerebro/sharetoken/`) — Persona was its only auth; public-share retired (Jesper: "bruger vi ikke").
- [x] persona handlers + routes (`handler/persona.go`, `persona_approvals.go`, `ListWorkspacePersonaAgents`, `UpdateAgentRuntimePersonaSandbox` + all `/api/persona/*` + `/persona-sandbox` route mounts).
- [x] SDK package `packages/cerebro-persona-sdk/` + `server/go.mod` `replace`/`require` (`go mod tidy`) + persona dev scripts.

Remaining (tracked separately, see §5.2 + the persona-sandbox DB columns): drop the now-inert DB
artefacts (`agent.persona_sandbox`, `agent_runtime.persona_sandbox` columns; `cerebro_persona_mask_audit`,
`cerebro_share_token` tables) via rename→verify→drop, and the frontend persona remnants.

### 5.2 Migrate before dropping — the grant table `cerebro_workspace_grant`
It is **"dead for now" in intent but live in code**: it is the backing store of the **new**
permission engine (`permissions/resolver.go:117`) **and** the live, deny-by-default
**credential** security boundary (`permission-system.md` row 1). It is read by
workspace-copy (`workspacecopy/copy_roles.go:135-189`), the grants API/UI, and the
`get_grant` agent tool (`firtal_gateway_tools_extended.go:1525`).

`cerebro_tool_policy` cannot today represent three columns the grant table carries:
`approval_required`, `time_window_*`, `classification_ceiling`. **None are exercised by any
live enforcement path** (no UI/seed creates such grants), so the recommendation is to drop
those features in the consolidation unless a concrete need surfaces.

**Blocking prerequisites before the table can be dropped:**
1. Give **credential** enforcement a home on the tool-policy chain. **IN PROGRESS (FIR-1609
   Phase 7 keystone):** `cerebro_credentials_policy.go` now consults the chain as an
   **Allow-source** — an explicit Allow row on `credential.<action>` / `cerebro-credential:<uuid>`
   grants access (`chainCredentialSignal` → `foldCredentialVerdict`), flag-gated behind
   `cerebro_credential_chain_grant` (default OFF). This is the safe inverse of a Base=Deny
   capability: rather than flip the chain's default to Deny for credentials (which the monotone
   fold cannot express via `q.Base` — an Allow can never loosen a Deny base), the grant is
   recognised **only** from an explicit authored Allow row (`Effective.DecidedBy != ""`), never a
   no-row Base=Allow default — so it can never open a default-allow hole on reveal, and the grant
   floor still supplies deny-by-default until grants are migrated. **Remaining before drop:**
   migrate existing `cerebro_workspace_grant` credential grants → tool-policy Allow rows
   (fail-closed if not 1:1), verify, then flip the flag on and retire the floor. Until that
   migration + verification, the table is still the only live secret gate.
2. Decide the fate of `approval_required` / `time_window_*` / `classification_ceiling`.
3. Repoint or remove: workspace-copy, grants UI/handler/CLI/MCP, the `get_grant` tool, the
   audit FK (`cerebro_workspace_grant_audit`).
4. **Then** drop `cerebro_workspace_grant` (+ `_audit`) and remove the grants package,
   queries, CLI (`cmd_grant.go`), MCP tools (`cmd_mcp_tools_grants.go`), and the persona shim
   (`internal/cerebro/persona/`).

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
3. **Credentials onto the chain** (Base=Deny), retiring the grant resolver for credentials.
4. **Persona removed** per section 5.
5. **Visible enforcement state.** The flags that decide whether policy rows are actually
   enforced (`cerebro_local_tool_policy(_enforce)`, `cerebro_approval_gate`) must be shown next
   to the policy tables — an admin setting Deny must see whether enforcement is on.

Until then: **do not assume the 5 interfaces are the whole story.** Before claiming "X is
ungated", find the runtime call site — it is very likely one of the section-4 code-only gates.
