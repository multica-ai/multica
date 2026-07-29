# Permission & access control — what an agent can and cannot do

> **FIR-3402 role/mandate contract.** Durable agent access can be authored as a
> direct layered policy decision or packaged into a reusable, versioned
> `cerebro_role` with an expiring or permanent `cerebro_role_assignment`.
> Both authoring paths enter the same Policy Decision Service; a Role is not a
> second resolver. Policy resolution expands only unexpired, non-archived
> bindings on every decision and records the Role name and version in its
> explanation. Every task receives an immutable
> `cerebro_task_mandate` tool snapshot, and both managed and local runtimes
> check that snapshot at each call. Migration 9147 removes the former direct
> per-agent grant store and the former pre-run pass store after preserving their
> effective policy. Credential, sandbox, repository and approval floors remain
> independent deny-by-default ceilings.

> **FIR-3403 runtime-tool consolidation.** Runtime inventory is read only from
> `cerebro_capability` plus its runtime subject/evidence rows. Runtime, agent,
> group, and user choices are authored only in `cerebro_tool_policy`. Migration
> 9148 preserves the former choices and irreversibly drops
> `cerebro_runtime_tool` and its grant/override tables; startup backfill and the
> former generated/query readers no longer exist. The former
> `cerebro_runtime_tool_grant_ui` switch and grant editor are removed as well;
> Settings → Permissions is the only authoring surface for these choices.
> Every `toolpolicy.Store` constructor now retains the database adapter used by
> generated queries, so active role assignments participate in every resolve
> path. Missing role storage is an error instead of a silent policy bypass.
> Migrated scopes survive verbatim: a legacy group/user grant keyed on a
> runtime becomes an Allow conditioned on that `runtime_id` (arg allowlist),
> and a role permission stores a LIST of rules per tool — resolution honours
> each rule's `resource_pattern` and `conditions`, never just its setting, so
> a scoped grant can never widen into a workspace- or tool-wide one.
> The live server also starts a daily, tighten-only governance sweep over role
> assignments; it reports expired, orphaned, and unused access in severity
> order. Permission audit rows use the same critical-to-low ordering.

**Read this before you touch anything that grants, denies, gates, or approves an
agent action.** Permission enforcement in this codebase is spread across many
subsystems, and they are easy to confuse. This document is the map.

> **The mistake this doc exists to prevent.** A visible control, a runtime
> inventory entry, or an agent description is not evidence of access. Gateway
> and supported local runtimes always resolve the canonical policy and the
> immutable Task Mandate before a call. Independent credential, sandbox,
> repository and approval ceilings then intersect that verdict and may only
> tighten it. Never add a second authoring or resolution path for one of those
> surfaces.

> **The rule for anyone refactoring this area.** Several of the live gates below
> look like ad-hoc legacy code worth folding into the tool-policy chain. They
> are live and load-bearing — especially credentials, which is **deny-by-default
> for agents**. If you move enforcement into a new system, preserve the current
> behavior **1:1**. "Cleaning up" a live gate can silently open a door that is
> closed today.

Line numbers drift; this doc cites files and functions. Grep for the function
name if a line moved.

---

## Mental model: two separate questions

The canonical user-facing explanation is **Why Access** on a permission's
detail page. It renders the same effective setting, deciding layer and cap that
the policy table returns. Agent instructions direct access questions to
`get_agent_capabilities`; descriptions and visible controls are never treated
as evidence of access.

1. **"Is the tool-policy chain deciding this call?"** — Always for Firtal
   Gateway tool exposure and calls through the Policy Decision Service. Other
   runtimes retain their separately documented rollout and enforcement paths.

2. **"Is this agent action gated at all, by anything?"** — Very often, yes, by
   one of the live subsystems in the table below.

Keep these apart. This doc answers question 2 honestly.

### One permission truth across every surface

For a concrete agent, member, runtime, task and resource, all user and agent
surfaces must project the same effective decision:

- **Settings → Permissions** authors direct rules and reusable Roles, and its
  **Why Access** detail names the deciding layer or Role version.
- **Capabilities** and `get_agent_capabilities` tell the agent what is available,
  allowed, approval-gated or denied using that same effective decision.
- The task transcript and `multica permissions task <id>` show the immutable
  Task Mandate captured for the run, including whether it is active or ended.
- `multica permissions explain` provides the same current explanation for an
  actor/resource outside the task snapshot.
- Claim-time listing and call-time enforcement use the same capability identity.
  A listed action that cannot be called, or a callable action missing from the
  brief, is a contract failure.

Service Tokens follow the same feature catalog and server switch, but represent
a machine identity with a deliberately read-only scope and their own expiry,
audit and revocation lifecycle. They do not widen an agent's tool permissions.

### Permission keys with a declared special contract

Eight keys deliberately differ from the ordinary Allow-baseline chain. Their
single contract registry is `server/internal/cerebro/platformaccess`; every
table/Explain/capability surface and every call-time gate resolves them through
`toolpolicy.ResolvePermission`: `hooks:read`, `hooks:write`, `hooks:enforce`,
`hooks:manage_managed`, `tools:personal-browser`, `tools:test-as-user`,
`manage_workspace_overrides`, and `manage_group_overrides`. Feature switches,
host conditions, workspace membership, target scope, self-target protection,
and credential checks remain independent safety intersections; they do not
select a second permission resolver for the same key.

### Create issue is an always-on platform action

`create_issue` is enforced before mutation for authoritative agent/task actors
by `server/internal/cerebro/platformaction` on REST/CLI, workspace HTTP MCP, and
the Firtal Gateway runtime. `Allow` creates normally and `Deny` returns
`platform_action_denied`. `Ask` creates one approval without mutating, then
resumes the exact request after approval: REST/CLI retries with the approval ID,
while Workspace MCP and Gateway wait in-process. The server atomically consumes
a one-shot approval only when workspace, agent, capability, resource, and ID all
match, so replay, rejection, mismatch, and expiry create nothing. Root issues
and sub-issues use the same capability and contract. Member-created issues and
internal system materialisation are not agent platform actions and remain
unchanged. This floor does not depend on approval-inbox availability or either
approval UI flag.

Agents can also explicitly raise a pending human decision with the
`request_approval` tool. Workspace MCP and the managed Firtal Gateway use the
same approval service and store the server-owned task, issue, chat session and
trigger-comment origin in the request context. The tool only accepts an
authoritative agent/task identity; it does not turn member calls into agent
requests or bypass the permission decision for the action being proposed.
Approvals with that origin context are displayed inline below the matching Chat
turn or Issue/Channel/DM comment. Requests without a triggering comment appear
at the top of the matching issue timeline; the Approvals page remains the full
workspace inbox and audit surface. The approval experience is visible whenever
either `cerebro_approvals` or `cerebro_approval_gate` is enabled: navigation,
inline query and realtime subscriptions use that same combined condition. This
prevents an active Ask gate from hiding the human decision path. Neither flag
disables the server permission floor.

> **FIR-2175 / FIR-3062 (flag `cerebro_member_override`, default ON):** when this general
> gate IS deciding a call (question 1), a workspace uses the
> *member-override* model — a member's own Allow/Ask/Deny overrides an inherited
> group/workspace default by specificity, so a member Allow can OPEN what their
> group denied (it can loosen, not only tighten). Both general gates use
> `Store.ResolveDeclared`, which selects the mode from the permission key and
> workspace flag; OFF selects the tighten-only mode.
> The deny-by-default floors below — **credentials (row 1), agent-browser sandbox
> (row 8), repo checkout (row 10)** and the approval cap — never consult this flag
> and stay strictly tighten-only, so it can never widen access to a secret, the
> sandbox, or a repo.
>
> **FIR-2175 phase 3 (display only, no flag):** the permission table now also
> renders a one-line plain-language explanation per capability (English +
> Chinese), sourced from the `platformcatalog` register and exposed as
> `description` / `description_zh` on each tool-policy row. This is display
> metadata — it does not change any gate, the resolution chain, or the flag above.
>
> **FIR-2351 (parity fix, no flag):** `ResolveMemberOverride`'s ceiling stage was
> missing `on_behalf_of` (the delegated task initiator, FIR-2441) — a workspace
> with `cerebro_member_override` on would silently ignore an on_behalf_of
> Deny/Ask that the tighten-only `Resolve` already honours. Fixed so both
> resolvers agree: `on_behalf_of` always tightens, on either resolver, and never
> loosens. No behavior change while the flag is off (the Firtal workspace today).
>
> **FIR-2351/FIR-3820 (resolver contract):** the two resolution algorithms live
> only behind `ResolveWithMode`. Public callers use `Store.ResolveDeclared` or
> `Store.ResolvePermission`; the key contract selects `ModeHardFloor` for
> credential, agent-browser, Registry, and repo-verbed permissions and only
> permits `ModeOpenable` for ordinary permissions. The former pure wrappers and
> `Store.ResolveGeneral` entry point are removed so a caller cannot bypass key
> classification by choosing a mode-shaped API.
>
> **FIR-2351 (workspace Deny = openable default, product decision 2026-07-06):**
> this REVERSES the earlier build-plan item "authored vs default deny" (which
> wanted a deliberately authored Workspace-Deny to stay a hard ceiling under
> `ModeOpenable`). The product decision is the opposite: **a workspace-level
> setting — authored or not — is a DEFAULT, and an explicit Allow authored at
> the Agent, Group, or User layer opens it.** Safety moves from resolution to
> the WRITE path: only owner/admin and holders of the two Manage-permissions
> capabilities below may author the opening rows. Three consequences, ALL
> behind `cerebro_member_override` (flag off = pre-decision behavior,
> byte-for-byte):
> 1. `ModeOpenable` lets an explicit **Agent-layer** setting open a
>    workspace-authored default when no explicit Group/User row caps it; an
>    explicit member row is still the agent's absolute ceiling, and Runtime /
>    on_behalf_of / System stay tighten-only (`chain.go`,
>    `TestResolveMemberOverride_AgentOpensWorkspaceDefault`).
> 2. The admin table's **Effective column resolves with the same mode the gates
>    enforce** (`Store.Table` reads the workspace-level flag; `tableRowMode`
>    keeps credential.*, repo./credential. verbed keys, and
>    `tools:agent-browser` on the tighten-only display) — before this the table
>    always showed the hard-floor fold, so a genuinely working override
>    displayed as Deny.
> 3. `ConnectionEndpointEffective` treats an explicit **workspace-layer** row on
>    a connection as a workspace-authored default (endpoint row beats the wide
>    row by specificity), decided after per-actor rows and before the
>    connection's `default_access`. A per-actor explicit Deny still revokes
>    instantly and on_behalf_of stays Deny-only.
> The deny-by-default floors (credentials, agent-browser sandbox, repo
> checkout, approval cap) never consult the flag and stay strictly
> tighten-only, unchanged.
>
> **FIR-2351 (delegated override capabilities, product decision 2026-07-03):**
> `cerebro_member_override` above is SELF-only — it lets a member's own row
> override their group, never anyone else's. A separate, distinct mechanism now
> exists for authoring ANOTHER member's access: two ordinary tool-keys,
> `manage_group_overrides` and `manage_workspace_overrides`. Unlike
> `manage_credential_access` (Base=Allow, tighten-only), these two resolve
> through the declared `human_opt_in_or_admin` contract in
> `platformaccess` via `Store.ResolvePermission` — **OFF by default for
> non-admin members; granted only by an explicit
> Allow at the User or Group layer.** A Base=Allow check here would make every
> workspace member hold override power with zero rows authored, exactly
> backwards for "a permission you GIVE to users" (caught in adversarial
> review, Tine, 2026-07-03, finding 1). Holding either lets that user author a
> LayerUser `/tool-policy` row for someone else (general tool-policy AND
> Connections share this one write path) — group-scope reaches only a user who
> shares a group with the actor, workspace-scope reaches any OTHER real member
> of THIS workspace (a target UUID that only resolves to membership in a
> different workspace is rejected — finding 2 of the same review). **Hard
> rule, enforced in `cerebroRequireDelegatedOverridePolicy` /
> `toolpolicy.CanAuthorDelegatedOverride`: neither capability may EVER be used
> on the holder's own row — self-target is always rejected**, independent of
> scope. Workspace owner/admin is a deliberate, documented exception and still
> bypasses both capabilities (including on their own row) — every other
> capability gate in this file (`manage_credential_access`,
> `manage_connections`, `create_local_runtime`) checks admin first too; this is
> not a gap in the self-target rule, which is scoped to capability holders, not
> to admins who already hold unrestricted authority. Credentials remain
> untouched by this — `credential.*` keys stay on `manage_credential_access`
> only.
>
> **FIR-2351 follow-up (renames + wider write scope + group-scoped WHEN,
> product decision 2026-07-06):** the two capabilities are now titled **`Manage
> permissions`** (`manage_workspace_overrides`) and **`Manage group
> permissions`** (`manage_group_overrides`) — keys unchanged, so existing grant
> rows keep working. Two scope changes in `RequireToolPolicyWritePolicy`:
> (a) a **Manage permissions** holder may now also author **Group- and
> Agent-layer** non-credential rows (`cerebroRequireManagePermissionsPolicy`) —
> that is how you grant a group or one agent access that opens a
> workspace-level default Deny; workspace/runtime/system rows stay
> owner/admin-only, and the User-layer self-target ban is unchanged.
> (b) **Manage group permissions** can be pinned to specific group(s): the
> write gate resolves the capability once per group the actor and target
> SHARE, threading `group_id` into `RequestContext.ArgValues`, so an
> `arg_allowlist` WHEN condition on `group_id` on the capability's Allow row
> limits it to the listed group(s) (`EnforcedConditionKinds` offers the arg
> term on this key). An unconditioned row keeps the any-shared-group reach.
>
> **FIR-2351 follow-up ("Disable" — a per-permission hard floor, product
> decision 2026-07-06):** after the openable default above, Jesper asked for
> the mirror image — a way to still make ONE specific permission a hard,
> unopenable floor for everyone but an owner/admin, rather than the workspace
> Deny always being loosen-able. A third workspace-only setting,
> `SettingDisable` ("Disable" in the UI), does exactly that: when a workspace
> row is Disable rather than Deny, **no Group/User/Agent Allow can loosen it**
> — it behaves exactly like the existing credential/sandbox/repo floors, but
> choosable per permission instead of hardcoded. Mechanics:
> - `chain.go`: `resolveOpenable`'s Stage A (member specificity) and the
>   Agent-opening exception both short-circuit when
>   `Settings[LayerWorkspace] == SettingDisable` (`workspaceDisabled`); Runtime /
>   Agent / on_behalf_of / System can still TIGHTEN further, same as any Deny.
>   `SettingDisable` is normalized to `SettingDeny` (`normalizeDisable`) before
>   it ever reaches `rank()`'s fold, so `Effective.Setting` never leaks anything
>   but Allow/Ask/Deny — Disable is an authoring-only value, never a verdict.
> - `table_connection.go` (`ConnectionEndpointEffective`): a workspace Disable
>   returns Deny immediately, before any per-actor row is considered and
>   regardless of the `cerebro_member_override` flag state — unlike an ordinary
>   workspace Deny, which that path defers so a per-actor Allow can open it.
> - Write path: `SettingDisable` is only valid at `LayerWorkspace`
>   (`toolpolicy.validSetting` + a same-name DB CHECK,
>   `cerebro_tool_policy_disable_workspace_only`, migration 9122) — authoring it
>   anywhere else is rejected before the DB. Workspace-layer writes already fall
>   through to `requireWorkspaceOwnerAdmin` in `RequireToolPolicyWritePolicy`,
>   so setting or clearing Disable is already owner/admin-only with no new gate
>   needed.
> - UI: the workspace-layer decision control (`WORKSPACE_SETTING_CHOICES` in
>   `tool-policy-table.tsx`) is the only picker that offers "Disable"; every
>   other layer's picker (agent/group/user) and the repo/credential group
>   controls (already hard floors, so Disable adds nothing there) are
>   unchanged.

---

### Mini-app Connection calls

`connections.call` is always the intersection of two independent ceilings.
`server/internal/cerebro/apps.CallConnection` first requires the published app
version to have an approved `integration` scope for that exact Connection. The
runtime resolver then re-checks the acting member: API endpoints use
`ConnectionEndpointEffective`, while MCP tools use `ConnectionToolEffective`.
Only `Allow` dispatches; `Ask`, `Deny`, lookup errors, undeclared MCP tools, and
transport errors fail closed. Stored Connection credentials remain server-side.

Saved-connection verification follows the same credential boundary. The
Connections edit screen receives only masked secret fields and includes the
connection ID when it runs **Test connection**. The backend resolves that ID
inside the current workspace and replaces masked or empty credential fields
with the stored values before probing; an explicitly entered replacement still
wins. The raw credential is never returned to the browser or included in the
probe result.

### Apps Collection access

App catalog listing and app open/use are deny-by-default outside the app owner
and workspace owner/admin. Other members reach an app only through a
`cerebro_folder_grant` on the app's Collection or an ancestor Collection. The
same workspace/member/group and inherited-grant rules used by Documents apply,
resolved by `cerebro_app_folder_grant_visible`. An unassigned app therefore
stays private to its owner and workspace administrators until it is placed in a
Collection with access configured in **Settings → Collections**. App runtime
SDK data routes reuse `apps.loadApp`, and `connections.call` applies the same
predicate before it reads approved scopes. The runtime iframe remains opaque
and receives no parent session or Connection credential.

App lifecycle actions are a separate default-deny group-capability layer:
`apps.create` controls app creation, `apps.manage` controls preview, publish,
retry, rollback, scope approval and Collection management, and `apps.delete`
controls deletion. Workspace owners/admins bypass this layer; other members
need the corresponding `cerebro_group_capability` row in addition to Collection
visibility where the action targets an existing app.

### Managed Pi harness

When the workspace flag `cerebro_pi_harness` is on, a Pi runtime receives one
task-scoped managed extension from `packages/cerebro-pi-harness`. The extension
is the only Connection registration path for that Pi process: user-supplied
extension and tool-selection arguments are removed before spawn. It exposes the
task token's Multica CLI/MCP surface over stdio and the claim's enabled MCP HTTP
Connections directly; API Connection credentials remain in the server-side
relay configuration and are never copied into the task environment.

Task tokens may read the workspace property catalog and set or unset property
values only on their bound issue. Property definition creation and editing
remain human-only. Authentication loads the task row and attaches its issue,
agent, and workspace scope before these routes run; a token whose task/agent
binding cannot be verified is rejected.

Before every managed tool call the extension asks the existing daemon
`/tool-policy/resolve` endpoint for the acting task's decision. `Allow`
dispatches, `Ask` dispatches only after the resolver returns a final approved
decision, and `Deny` does not dispatch. In `enforce` stage, a missing or invalid
policy response fails closed. A failed call marked as a mutation is never
automatically retried because the external outcome may be unknown.

The harness is also Pi's **mandatory tool-policy adapter**. Pi has no
provider-native before-tool hook file and no built-in MCP, so the extension's
`pi.on("tool_call")` gate is the only call-time enforcement point Pi has;
`localtoolpolicy.ProviderAdapterFor("pi")` therefore reports it as a `Harness`
adapter and `prepareToolPolicySpawn` writes no settings file for it. Because the
gate lives in the extension, turning the workspace flag off no longer falls back
to an unenforced Pi spawn — `preparePiHarness` refuses to start Pi at all and
names `cerebro_pi_harness` in the error. Other providers are unchanged.

Pi's probe measures the Multica MCP channel, which by construction lists
Multica's tools and none of Pi's own — Pi dispatches those from an internal
registry that never crosses the MCP wire. `providerInventory.Native`
(`server/internal/daemon/cerebro_runtime_tool_probe.go`) is unioned onto the
measured result to cover that blind spot; without it `bash` had no capability
row, the task mandate's allowlist had nothing to match, and every shell call was
denied before the policy chain was consulted. A failed probe still reports *not
measured* rather than a Native-only inventory, so a measurement failure never
reads as a permission decision.

### ACP-gated runtimes (hermes, kimi, kiro)

These three drive one ACP client (`server/pkg/agent/hermes.go`). The Agent
Client Protocol requires the agent to ask its CLIENT for permission before it
runs a tool — `session/request_permission` — and the daemon IS that client, so
the before-call seam exists without any provider-native hook file or installed
extension. `localtoolpolicy.ProviderAdapterFor` reports them as `ACPClient`
adapters and `prepareToolPolicySpawn` writes no settings file. Because the gate
is in-process in the daemon, it cannot be disabled by removing a file from the
workdir.

`server/pkg/agent/cerebro_acp_tool_policy.go` resolves each request through the
same `Config.ToolPolicy` callback Codex's approval seam uses. Two properties
make it a real gate:

- The runtimes are launched WITHOUT their auto-approve modes. `HERMES_YOLO_MODE`
  and Kiro's `--trust-all-tools` suppressed the permission request entirely and
  were the actual enforcement hole.
- The gate answers only once-scoped options and never an `allow_always` kind.
  An always-scoped approval tells the agent to stop asking, which would end
  enforcement for the rest of the session.

A missing policy callback, unparseable params, or an options list with no usable
answer all deny (an ACP `cancelled` outcome where no rejection option is on
offer).

### OpenCode plugin adapter

OpenCode exposes no provider-native hook file, but it loads plugins from
`<workdir>/.opencode/plugin` and fires `tool.execute.before` ahead of every tool
it runs; throwing from that hook aborts the call. The Firtal plugin in
`packages/cerebro-opencode-harness` is therefore OpenCode's mandatory adapter,
resolving each call against the daemon's `/tool-policy/resolve` endpoint and
failing closed on a missing daemon port, a non-OK response, or a transport
error. `prepareOpenCodeHarness` installs it per run and refuses the spawn when
it is not on disk, and rewrites it every run so a stale or tampered copy in a
reused workdir cannot survive.

## What is enforced LIVE today (no flag required)

Browser credential provisioning has an additional always-on floor:
the agent owner's membership in the `browser-testers` group plus an explicit
`credential.reveal` Allow on the exact
`agentvault-vault:Shared/browser-login/<app>` resource. Personal-browser
`secure-fill` returns the value only to the trusted desktop bridge for direct
Chromium injection. `multica agent-browser provision-auth` is task-token-only;
its process-local bridge reads the two values, passes the password to
`agent-browser auth save` through stdin, suppresses child output, and returns
only non-sensitive profile/vault/key metadata to the invoking agent.
`multica agent-browser internal-verify` uses the same exact-box grant. Targets
may request either a form login, Cloudflare Access headers, or both. Access-only
targets never read `USERNAME` or `PASSWORD`; their allowlisted target definition
names the exact Agent Vault keys to reveal. Atlas uses this to keep separate
Admin and Reader service-token pairs in `Shared/browser-login/data-catalog`,
while its unknown check receives no credential.

Every row here actively allows or denies an agent action right now, with default
configuration. Verified against the code.

### Canonical runtime-tool path (FIR-3403)

Runtime-tool access no longer has a separate enable/grant cascade or a
server-side rollout switch. The authoritative path is:

1. `cerebro_capability` plus runtime subject/evidence rows records what the
   runtime can actually provide. `GET /api/runtimes/{id}/tools`,
   `GET /api/runtimes/{id}/tools/effective`, and
   `POST /api/runtimes/{id}/tools/scan-now` are inventory/diagnostic routes;
   they do not author access.
2. `cerebro_tool_policy` is the only runtime/agent/group/user authoring store.
   The policy table, the runtime executors, `get_agent_capabilities`, and the
   injected tool brief all resolve the same register and policy rows.
3. Connection discovery remains compatible with scan-now: MCP `tools/list`
   evidence is registered as capabilities, while per-connection and per-tool
   decisions continue through `ConnectionToolEffective` or
   `ConnectionEndpointEffective`.

The older row-by-row implementation history below predates this consolidation.
Where it mentions `cerebro_runtime_tool`, runtime grant/override routes,
`agent_tool_grant` as a broad exposure cascade, or a server rollout switch,
that text is historical context rather than the current contract. The three
points above are authoritative for runtime-tool inventory and access.

> **FIR-3388 Connection resolver correction.** Historical text in row 2b
> mentions `connectionWideBases` and `connectionWideAuthoredBases`. Those
> parallel table folds are retired. Connection-wide policy now resolves through
> `Store.ResolveDeclared`, and exact tool/endpoint rows use the same
> Role-aware resource resolver as repository and credential rows. This applies
> even when an API Connection has no capability-register row.

| # | What is gated | Mechanism / where | Default behavior |
|---|---|---|---|
| 1 | **Credential access** — attach, read-redacted, reveal, rotate, revoke a secret | `server/internal/cerebro/credentials/service.go` (`enforce`), policy chain in `credentials/policy.go` + `server/cmd/server/cerebro_credentials_policy.go`; wired unconditionally in `router.go` via `.WithPolicy(...)` | **Deny-by-default for agents.** Workspace owner/admin (members) pass; every non-owner hits the deny-by-default floor — credentials are **owner-only** in practice (secrets live in Infisical + agent vault). The constant legacy grant resolver and its empty SQL read are retired. The canonical credential path starts with a direct Deny floor for non-owners, then evaluates the tool-policy chain once per scope; owners and admins still pass the owner policy. Plaintext is never decrypted on deny; every attempt is audit-logged. This is a real security boundary. **FIR-1609 Phase 7 keystone (`chainCredentialSignal` + `foldCredentialVerdict`):** the unified tool-policy chain is consulted once per scope (id, then type) and plays TWO roles. Always-on: a Deny/Ask row TIGHTENS the floor (cap, Base=Allow ⇒ no rows ⇒ no cap). Flag-gated behind `cerebro_credential_chain_grant` (default OFF): an **explicit Allow row** (`Effective.DecidedBy != ""`, never a no-row Base default) GRANTS access as an Allow-source. While the flag is OFF, or with no credential rows, the verdict is byte-for-byte the prior `foldCredentialCap(floor, cap)` (proven by `TestFoldCredentialVerdict_NoGrantIsPreviousBehaviour`), and an explicit Deny/Ask cap always overrides a chain grant — so this can never widen who reveals a secret by default. **The per-actor "credentials" authoring surface (FIR-1479, flag `cerebro_credentials_per_actor`) now lives ON this tool-policy chain itself: `toolpolicy/table_credential.go` emits one group of rows per credential box (`credential.attach|read_redacted|reveal|rotate|revoke`) on all actor layers, so authoring and enforcement read the SAME store. **FIR-1739 — the box list unions two sources:** credentials registered in `cerebro_credential` (ResourcePattern `cerebro-credential:<uuid>`) **and**, when an Agent Vault vault lister is wired into the Store (`Store.WithVaultLister`, set only for the Credentials-tab handler in `router.go` via `agentvault.NewVaultLister()`), the workspace's Agent Vault boxes (ResourcePattern `agentvault-vault:<name>`, the vault-level grant `agentvault/mirror.go` + the resolver already honor — no `cerebro_credential` row needed). A box present in both is listed once (the id-scoped registered row wins). This is what surfaces grantable credential rows for a workspace like `firtal` whose secrets live ONLY in Agent Vault (empty `cerebro_credential`); the lister is optional + nil-safe and degrades to no Agent-Vault rows when admin creds are absent. The earlier parallel store (the `cerebro_credential_policy` table + the `credentialpolicy` package + the agent-centric `/api/agents/{id}/credential-grants` API + `AgentCredentialGrantsPanel`) was authoring/display only — `Check` never read it — and is now RETIRED (the table is left orphaned, non-destructive). Writes to `credential.*` rows are gated by the `manage_credential_access` capability — anyone with "Grant credential access" — while a LayerUser row on any OTHER key may instead be authored by a holder of `manage_group_overrides`/`manage_workspace_overrides` (FIR-2351, self-target always rejected); every other write (Group/Workspace/Runtime/Agent/System layer) stays owner/admin-only (`RequireToolPolicyWritePolicy`). Whether an explicit Allow row on this chain actually GRANTS credential access is still gated by `cerebro_credential_chain_grant` (default OFF), as described above.** |
| 1a | **Credential-use audit trail** — what an agent did with a secret, and on which issue | `server/internal/cerebro/credentials/service.go` (`AuthorizeRead`, `auditAllow` / `auditDeny` / `Reveal` → `RecordCerebroCredentialAudit`) | **FIR-2243 (B3).** Two enrichments to the existing `cerebro_credential_audit` trail. (1) Every audit row now records the **issue/task** the action happened on — folded into the existing `metadata` JSONB as `issue_id` / `task_id` from the task token's `TaskScopeContext` (member / non-task calls add nothing). (2) A **successful read by an agent** now writes a `read` / `allow` row; member (UI) reads stay unlogged, preserving the JEH-1197 rationale that one row per page-render would balloon the table. Together this makes "which key did which agent use, on which issue" answerable by cerebro evaluate / `firtal-data-registry`. The readable key name is obtained by joining `cerebro_credential` on `credential_id`. No schema migration — uses the existing `metadata` column. |
| 1b | **Workspace service tokens (`msv_`)** — read selected workspace areas from an external system | `server/internal/cerebro/servicetoken` + `/api/service/{skills,agents,issues}`; management at Settings → Tokens and `/api/service-tokens` | **Read-only and fail-closed (FIR-3754).** Only `skills:read`, `agents:read`, and `issues:read` can be minted; the machine surface has no write route. Expiry is mandatory (1–365 days), enforced in the service and database. The workspace-level `cerebro_service_tokens` flag is the single kill switch for management, UI visibility, and every existing token: an explicit workspace OFF disables all three, no override uses the registry default ON, and lookup errors fail closed. Every authenticated request must durably append a `used` audit row with method and path before access continues; an audit write failure rejects the request. Migration-triggered revocation and write-scope removal are audited atomically with their state changes. |
| 2 | **Which tools an agent gets at all** | `runtime.policyDecisionTools` and `runtime.guardToolCall`, backed by the live per-task Registry, the capability catalog, and `cerebro_tool_policy`. Built-in tool implementations live under `server/internal/cerebro/runtime/*tools*.go`, including the customer-service MCP proxy tools. | Resolved live per agent on every Gateway task. A tool must exist in the live Registry and its canonical policy verdict must permit exposure; missing identity, missing callable, lookup error, or Deny fails closed to chat-only/no tool. Ask may be listed but requires approval at call time. There is no rollout switch, cascade, or direct per-agent grant fallback. Connection, credential, sandbox, repository, and platform-action floors remain additional ceilings. |
| 2a | **Effective runtime tool access preview** | `server/internal/cerebro/toolaccess` + `handler.ListRuntimeToolEffectiveAccess` on `GET /api/runtimes/{id}/tools/effective`; runtime protocol data comes from the canonical capability snapshot (`agent_runtime.capabilities`, normalised by `runtime_capabilities_cerebro.go`) | **Read-only preview, not an execution gate.** It combines capability inventory, the canonical tool-policy resolver, protocol compatibility (`native_tool_loop`, `mcp_stdio`, `mcp_http_sse`, `managed_http_tool_loop`, or `chat_only`), Ask support, and credential risk. Runtime Tools and Agent Tools use it to explain exposure or closure; Settings → Permissions is the only writer. The tool-policy table also identifies blocking groups and owner so a capped override is explained rather than shown as effective. |
| 2c | **Agent self-capability lookup** — an agent reads its OWN capabilities card | `GET /api/agents/{id}/capabilities` registered in the task-scoped allowlist in `router.go` via `AllowTaskScopeForAgent("id")` (handler `GetAgentCapabilities`, `agent_capabilities_card_cerebro.go`); the `get_agent_capabilities` tool is callable by agents (`runtime/tools_registry.go`, `ToolStatusNewlyImplemented`) and defaults `agent_id` to the caller (`clitools/mcp_tools_agent_capabilities.go`, via `MULTICA_AGENT_ID`) | **Self-only, live (FIR-2243).** An agent's `mat_` task token can fetch the capabilities card **only for its own agent id** — any other id returns 403, enforced by `AllowTaskScopeForAgent` (mirrors `GET /api/agents/{id}/tools`, row 2). User/admin tokens are unchanged. Secret/credential **names stay redacted** for the agent caller (`mayRevealAgentSecretNames` is false for non owner/admin). This lets an agent discover what it can do, may use, has access to, and is limited by — so it knows where to look — without being able to read any other agent's card. |
| 2b | **Workspace Connections MCP inventory + per-tool deny** | `server/internal/cerebro/connections.Store.BuildMCPConfig`, merged into task claim in `handler/daemon.go` and daemon scan config in `handler.GetRuntimeMcpConfig`; per-tool rows and denies resolve through `server/internal/cerebro/toolpolicy/table_connection.go`; daemon spawn enforcement lives in `server/internal/daemon/daemon.go`; workspace-scoped HTTP MCP entrypoint lives at `server/internal/handler/workspace_mcp_cerebro.go` and is mounted as `POST /api/workspaces/{id}/mcp` behind `RequireWorkspaceMemberFromURL` | Enabled `workspace_connection` MCP servers are injected into runtime MCP config and included in daemon `tools/list` scans. **FIR-2341 (`daemon-claim-connections-injection` CEREBRO-PATCH):** prior to this fix, `h.ConnectionsInjector.BuildMCPConfig` was called only in the scan path (`handler/runtime_tools_scan_cerebro.go`) but NOT in `ClaimTaskByRuntime` (`handler/daemon.go`). Local daemon runtimes could see workspace `mcp_http` connections during a tool scan yet never receive them in `--mcp-config` at task claim time — so agents could list the tools but Claude Code never had the servers wired. The patch mirrors the scan-path injection into `ClaimTaskByRuntime`, using `daemonmcp.Merge(connMCP, agentStaticConfig)` so agent-static config wins per server name. API-type connections (executed server-side) are unaffected — they are never sent in `--mcp-config`. The scan records runtime tool evidence in the canonical capability register; Workspace, Runtime, Agent, Group, and User decisions are authored in `cerebro_tool_policy`. TECH-3156 adds a narrower live gate for MCP connections: the Connections tab now emits per-connection tool rows from the persisted `workspace_connection.tools` inventory, and effective **Deny** rows are resolved at task claim via the same Workspace › Runtime › Agent › Group › User chain the UI shows. `ClaimTaskByRuntime` returns those deny tokens as `disallowed_mcp_tools`, and the local daemon copies them into `agent.ExecOptions` before spawning the provider. For Claude Code runs, denied connection tools are passed as `--disallowedTools mcp__<connection-name>__<tool-name>`, so a denied MCP tool is not callable. If deny resolution errors, the claim **fails closed** by withholding all workspace connections for that claim instead of injecting them without enforcement. **FIR-1459:** local Cursor and Gemini runs now materialise the merged MCP config into provider-native project files before spawn (`.cursor/mcp.json` and `.gemini/settings.json`, restored after the run), so workspace connections are available to those local runtimes instead of being silently dropped by the backend. Denied connection tools stay per-tool where the provider supports it: Cursor gets `Mcp(<connection>:<tool>)` entries in `.cursor/cli.json` `permissions.deny`, and Gemini keeps the server but receives per-server `excludeTools` (with Multica HTTP MCP `url` translated to Gemini `httpUrl`). Providers without a native per-tool MCP deny still fail closed by stripping the whole connection when any tool on it is denied. REST/API connections also surface per-endpoint+method rows from `endpoint_permissions` for admin CRUD configuration. **FIR-2166 "C" (PR1):** the engine half of REST enforcement now exists — `toolpolicy.Store.ConnectionEndpointEffective(workspace, runtime, agent, user, connName, method, path)` folds the connection-wide cap (`connection:<name>`, empty pattern) and the per-endpoint row (`source='connection-endpoint'`, pattern `"<METHOD> <path>"`) through the full Workspace › Runtime › Agent › Group › User chain. **FIR-2166 "C" v2 — per-connection configurable default:** each connection carries a `default_access` mode (`allow`/`ask`/`deny`, column `workspace_connection.default_access`, migration 9107, default `deny`) chosen on the connection form. `ConnectionEndpointEffective` resolves per-actor rows with precedence **Deny > Allow > Ask**, then falls back to that connection default (a missing row/column resolves to Deny — fail closed, via `connectionDefaultAccess`). This is NOT the tighten-only `Resolve` (which could never lift a Deny default with an Allow grant — the `ResolveOptIn` rationale, FIR-1771). A server-side-dispatched API connection can reach internal URLs with a stored credential the agent never sees (e.g. infisical-admin fronts the secrets box), so such a connection is set to `default deny`: "allow only Sara & Mia" is then an agent-layer Allow on `connection:<name>` per agent, and every other — and every NEW — agent is denied; a harmless API can be `allow` (open, deny to restrict) or `ask` (approval-gated). Independent of MCP connections (`ConnectionToolEffective`, Allow-baseline + Deny-to-restrict), which are unaffected; the admin screen reads the same resolver so it cannot drift. The per-endpoint rows still inherit the connection-wide cap as their resolution Base: `connectionWideBases` reuses a capability-derived wide row when one exists, and `connectionWideAuthoredBases` resolves the authored `connection:<name>` (empty-pattern) settings straight from `cerebro_tool_policy` as a fallback so the connection-wide Deny/Ask cascades to endpoints even for API connections that have **no** `cerebro_capability` row (authored only in `workspace_connection`). **PR2 (`runtime/api_connection_tools.go`):** behind the default-off workspace flag `cerebro_api_connection_tools`, every enabled API-type connection's allowed endpoints are now exposed to firtal-gateway agents as tools — one tool per `<METHOD> <path>`, name `"<connection>__<method>_<path>"`, JSON schema derived structurally from the endpoint (required string per `{param}` path placeholder, a free-form `query` object, and a `body` object for body methods). `APIConnectionTool.Call` dispatches the request SERVER-SIDE from the Cerebro backend to the connection URL with the connection's `auth_config` applied (bearer / api-key header / CF-Access service token), so the credential never reaches the agent and `.internal` URLs are reachable directly (no daemon relay). The flag is read via `FirtalGatewayExecutor.apiConnectionToolsEnabled` (fail-to-OFF) and the tools are registered into the per-task registry in `runToolLoop`. **PR3 (`runtime/api_connection_enforcement.go`):** the API-endpoint tools PR2 exposes are now enforced PER ACTOR through `ConnectionEndpointEffective`, mirroring the MCP-connection enforcement below. Two halves: (1) **list time** — the list-time filter (originally `filterDeniedAPIEndpoints` in `runToolLoop`; that function is gone today, superseded by `APIConnectionResolver.ListForAgent`, see the FIR-2388 paragraph below) drops from the listed tool set every endpoint whose effective verdict is **Deny** for the agent; under the v2 opt-in default that is everything the agent was not explicitly granted (a model never sees a tool it cannot call), while all endpoint tools are still *registered* so the call-time guard can resolve them. (2) **call time** — `guardToolCall` (runtime/approval_gate.go) takes the per-task `*Registry`, looks the tool up, and for an `*APIConnectionTool` folds `apiEndpointSetting` (→ `ConnectionEndpointEffective` for the agent's runtime + owner) into the same `connSetting` as MCP connection tools via `MoreRestrictive`: **Deny** blocks always-on (independent of approval-inbox availability, because gateway dispatch never sees `--disallowedTools`). The two halves originally failed **differently** on a resolve error (logged): list time failed **open** to Allow (keep the tool listed — the always-on call-time guard is the real check), while call time fails **CLOSED** to Deny (FIR-2166 C review) because this gate fronts the secrets box, so an unresolved verdict must never grant a call. **(FIR-2388 flipped the list-time half to fail-CLOSED too — a per-endpoint policy error now DROPS the endpoint from the listed set on every surface. See the FIR-2388 paragraph at the end of this cell.)** (Opt-in never yields Ask for API endpoints, so the call-time Ask path is unreachable here; it remains for MCP connection tools.) This is all still behind the default-off `cerebro_api_connection_tools` flag: with the flag off PR2 exposes no API tools, so there is nothing to enforce. TECH-3108: connection capabilities are workspace-level and never runtime-reported, so the connection-wide row (`source='connection'`) is exempt from the runtime EXISTS filter in `toolpolicy/table.go` — without that exemption the Connections tab rendered empty on the runtime and agent permission views (the per-tool rows already bypass the filter). This is a UI-visibility fix only; the resolution/enforcement chain is unchanged. TECH-3180: the connection-wide row is now also **enforced**, not just visible. Its resolved Effective (the full Workspace › Runtime › Agent › Group › User chain) is used as the per-tool resolution Base in `toolpolicy/table_connection.go` (`connectionWideBases`), so a Deny set on the whole connection at the **runtime** or **agent** layer cascades to every MCP tool on that connection and surfaces in `DeniedConnectionTools` as deny tokens at claim — scoped to exactly that runtime/agent (a different runtime/agent keeps access). Per-tool rows still tighten under the connection-wide setting; they never loosen it. **TECH-3498:** **Ask** on a connection tool is now enforced, not just stored. The Deny-only resolver is generalised to `toolpolicy.Store.ConnectionToolEffective`, which returns the full Allow/Ask/Deny verdict through the same chain, and both engines route an Ask verdict through the existing shared approval inbox (the same `permgate` gate that backs every other Ask): on the **firtal-gateway** engine `guardToolCall` (runtime/approval_gate.go) blocks on Deny always-on and, when the workspace approval inbox is enabled, raises an inbox ask on Ask and blocks until a human approves; on **Claude Code daemons** the tool-policy resolve endpoint (`handler/daemon_tool_policy_cerebro.go`) folds the connection verdict into its result by tightening, so a connection-tool Ask drives the PreToolUse hook's existing inbox + long-poll path. Enforcement of Ask therefore requires the approval inbox to be active (gateway: `cerebro_approval_gate` on; daemon: local tool-policy in enforce mode) — with the inbox off, Ask has no effect, exactly like every other Ask. Deny remains always-on on the gateway regardless of the inbox. From the approvals inbox an admin can now resolve a connection-tool Ask once, for a period (a time-boxed grant via `expires_at` that `FindReusable` honours so subsequent calls reuse it until it lapses), or escalate it to a permanent **Allow** at the agent, runtime, or workspace level (written into the tool-policy chain via `approvals.Handler.grantAtLevel` as a best-effort secondary write that never fails the approve). The approval inbox is **human-readable**, not ID-based: the ask `Context` carries `issue_id` (from `meta.IssueID`, set in `runtime/approval_gate.go` `askContext`) plus `trigger_user_id` / `trigger_user_name` (from `meta.TriggerUserID` / `meta.TriggerUserName` — the human who triggered the run), and `approvals.Handler.enrich` resolves requester / decided-by / delegated-to **names**, the **issue identifier + title**, and the **triggering member** (`triggered_by_name`, with a task `OriginalUserID` fallback when no trigger context is stored) at read time (best-effort; raw UUIDs never surface in the UI) — this is a display-only enrichment, the resolution/enforcement chain is unchanged. TECH-3405 adds a first-party HTTP MCP endpoint per workspace. It is still protected by target-workspace membership credentials; a task token bound to workspace A cannot call workspace B directly because `middleware/workspace.go` rejects the mismatch before membership lookup. A workspace connection can intentionally target another workspace only by storing a bearer credential that is valid in that target workspace, after which the connection deny chain can allow/block the exposed MCP tools for source runtimes/agents. The first tool exposed there is `create_issue`, which goes through `IssueService.Create` so parent/project workspace checks, duplicate guard, broadcasts, analytics, and assignment triggers stay aligned with normal issue creation. **TECH-3174:** the `--disallowedTools` path only covers Claude Code daemons. Agents on the **firtal-gateway** runtime dispatch connection tools server-side (e.g. the built-in customer-service MCP tools in `runtime/customer_service_mcp_tools.go`), so a second, **always-on** check enforces the same per-tool Deny there: `FirtalGatewayExecutor.guardToolCall` (runtime/approval_gate.go) calls `toolpolicy.Store.ConnectionToolDenied` for every tool call — independent of approval-inbox availability — and blocks the call when the tool name matches an effective Deny on any `connection:<name>` (`resource_pattern == tool name`) through the full chain. Fail-open + warn on a DB error so a transient blip cannot take the gateway fleet offline. **FIR-2388 — one resolver, "listed == callable == shown in the brief".** The three surfaces that each filtered the api-type connection endpoint list their own way (cloud gateway = Allow+Ask fail-OPEN; local `connectiontools` HTTP handler = Allow-only fail-CLOSED; claim-time brief = never listed api-connection tools at all) now all call ONE shared filter, `runtime.APIConnectionResolver.ListForAgent` (`runtime/api_connection_resolver.go`): keep **Allow + Ask**, drop **Deny**, and **fail CLOSED** on a per-endpoint policy resolve error (drop the endpoint) — this is the list-time flip noted in the PR3 paragraph above. It lives in the `runtime` package (not `connectiontools`) to avoid an import cycle, and the handler-package brief reaches it through the injected `handler.CerebroAPIConnectionBriefResolver` interface (`runtime/api_connection_resolver_brief.go`), so the brief lists exactly the tools the agent can call — closing the gap where an agent HAD an endpoint but never saw it in its prompt. Cloud registration now seeds only Allow+Ask endpoints into the per-task registry (Deny endpoints are no longer registered). Still behind the default-off `cerebro_api_connection_tools` flag (ListForAgent returns nil when off). **The call-time guard is unchanged as the hard enforcement** — `guardToolCall` re-resolves each call via `apiEndpointSetting` (fail-closed). **New Ask hardening (`approvalInboxActive`, `runtime/approval_gate.go`):** an API-connection ENDPOINT whose verdict is **Ask** fronts the secrets box and is dispatched server-side, so it MUST reach the approval inbox before it runs. If the inbox will not run for the call (gate nil or workspace approval flag off), the call now **fails CLOSED** instead of being silently downgraded to Allow — mirroring the local MCP path that 403s an Ask endpoint. When the inbox IS active, the Ask still flows to `guardConnectionAsk` and blocks for a human as before. (This differs from an MCP-connection Ask, which is a no-op when the gate is off per TECH-3498, because an MCP Ask is relayed to the daemon rather than dispatched server-side against a stored credential.) **`multica_connection_tools_status` tool:** a read-only diagnostic tool (matrix `ToolStatusExcluded`, inventory read-only exemption in `permguard/inventory.json`, matched in `MulticaMCPToolMatrix`) so that an agent granted **zero** callable connection tools sees an explicit "0 connection tools" status instead of silence; it performs no API mutation and returns no secret values, so it carries no access of its own. **FIR-2441 #2 — relay-level always-on Deny for local `mcp_http` (`server/internal/cerebro/mcprelay/relay.go`).** The `--disallowedTools` deny only binds CLIs that honor it (Claude/Cursor/Gemini); Codex/Hermes/Kimi/Kiro/OpenCode ignore it, so a Deny leaked for those local runtimes even though the tool was in the disallow list. The MCP relay — the path EVERY local `mcp_http` call already flows through — now inspects each `tools/call` and returns `403` when the called tool's effective verdict is **Deny** (`mcprelay.chainToolPolicy` in `module.go`, implementing the `toolPolicy` interface in `relay.go` → `toolpolicy.Store.ConnectionToolVerdicts`), independent of the agent CLI **and** of the `cerebro_api_connection_tools` flag. It **fails open** on any non-Deny verdict or a body it cannot safely buffer/parse. **FIR-3820 (audit finding A2) flipped the resolver-ERROR case to fail CLOSED:** a policy resolve error now rejects the `tools/call` with `503` instead of proxying it. This gate is the only thing enforcing a Deny for the CLIs that ignore `--disallowedTools`, so proxying on a DB error would silently un-enforce exactly the Denies it exists to protect. The relay token binds only `(workspace, connection)`, so this enforces the **workspace** actor level today; agent/member-level relay deny needs the actor identity carried in the token (follow-up slice — until then those layers stay enforced at list-time via the Allow-only withholding when the flag is on, and `--disallowedTools` for the CLIs that honor it). **FIR-2564 fase 2 — per-person session keys on api-type connections (`runtime/api_connection_session_exchange.go`).** An api-type connection whose `auth_config.session_exchange.enabled` is set no longer dispatches data calls on the shared connection key when the run has a triggering human: `APIConnectionTool.Call` first exchanges the shared key for that person's own short-lived session key (`POST <url>/sessions/exchange`, the Firtal Data Registry contract — the remote side authorizes the exchange against the key's delegation allow-list) and runs the call on the personal key, so the remote API enforces THAT person's access and revoking one person stops exactly that person. The person is ONLY the triggering human (`GatewayRequestMeta.TriggerUserID`, carried via `WithConnectionTriggerMember` from `runToolLoop`/`runToolLoopWithServer`); the agent's owner is never substituted, so a run triggered by X can never borrow Y's access. Failure posture: no triggering human (system runs, and the local `connectiontools` surface which carries no trigger identity) → shared key as before; exchange enabled + human known + exchange fails → the call **fails CLOSED** (never a silent fallback to the broader shared key). Exchanged keys are cached AES-256-GCM-encrypted in `cerebro_connection_person_key` (migration 9115, `MULTICA_CREDENTIALS_KEY` cipher from `internal/cerebro/credentials`), reused until 60s before expiry, and both the personal and the shared key are redacted from tool responses. **FIR-2668 — per-agent delegation on api-type connections (`runtime/api_connection_on_behalf_of.go`).** An api-type connection whose `auth_config.on_behalf_of.enabled` is set stamps the calling agent onto every server-side dispatch as the `X-On-Behalf-Of: agent:<uuid>` header (the Firtal Data Registry delegation contract, FIR-2564 fase 1): the remote API authorizes the call as THAT agent's own grants — its key's `delegation_allowlist` must explicitly cover the agent, fail closed remotely — so the shared connection key stays inside the backend, needs no data grants of its own, and per-agent access is granted/revoked remotely without handing keys to runtimes. The agent identity is ONLY the authenticated caller (`GatewayRequestMeta.AgentID` via `WithConnectionAgent` on both gateway tool loops; the `mat_`-token-resolved identity on the local `connectiontools` surface) — never a value the model controls. No agent on the context (system/human surfaces) → the header is not sent and dispatch is unchanged. For agent callers on_behalf_of takes precedence over `session_exchange`: a delegated agent call runs on the shared key + header, never on the triggering human's exchanged session key, so an agent cannot borrow the human's broader access. |
| 3 | **Whether an agent gets tools at all (outer gate)** | `runtime/firtal_gateway_executor.go` (`agentHasCallableTools` → `policyDecisionTools`) | **Chat-only when the canonical decision returns no callable tools.** The live Registry provides availability and `cerebro_tool_policy` provides the decision. An all-Deny policy, missing Registry entry, or resolution failure cannot be reopened by a fallback list or server switch. |
| 4 | **Excluded tools (~47)** | `runtime/tools_registry.go` — `ToolStatusExcluded`; `callableBuiltinToolNames()` registers only `Implemented`/`NewlyImplemented` | Excluded tools are **never registered**, so no agent can call them regardless of grants. Count drifts as tools land; check the source. |
| 4b | **Skill self-learning tools (`skill_record_observation`, `skill_get_observations`, `skill_set_auto_learn`)** | `runtime/tools_registry.go`; recording handler `handler/skill_learning_cerebro.go`; toggle handler `handler/skill_autolearn_cerebro.go`; routes and gates are inventoried in `permguard/inventory.json` | TECH-3077 / TECH-3692 — observation recording and reading feed the learning aggregates. `skill_get_observations` is read-only and `skill_record_observation` only writes observation rows. `skill_set_auto_learn` changes the target skill, so `POST /api/skills/{id}/auto-learn` uses the existing skill-ownership gate (owner, approver, or workspace admin). Turning the switch off makes `RecordSkillObservation` skip that skill. The downstream proposal sweeper remains gated by the default-OFF `cerebro_skill_learning` workspace flag, and proposals remain pending until normal governance approval. |
| 4c | **Agent Office context-governance tools (`agent_context_versions` / `_change_requests` / `_propose` / `_review` / `_diff` / `_rollback` / `_set_ownership`)** | `runtime/tools_registry.go` — all seven `ToolStatusExcluded` in the matrix; actual handlers are CLI-runtime MCP tools in `clitools/mcp_tools_agent_office.go` over the `/api/agents/{id}/context/*` routes, gated per `permguard/inventory.json` (read tools `exempt`, write tools `gate: via:<route>`) | FIR-1775. These are the agent analog of the `skill_*` governance tools (all of which are also matrix-`Excluded`): they let an agent list/propose/review/diff/rollback an agent's versioned context **through the API's own route gates**, not as built-in runtime tools. Registering them in `MulticaMCPToolMatrix` as `Excluded` is a **display/inventory-parity** entry only (so `TestMulticaMCPToolMatrixMatchesInventory` holds) — it grants no callable runtime tool and changes no enforcement: a write still requires passing the route's permission gate, and a propose/rollback applies nothing until the review/approval step. |
| 4d | **Skill-governance gap tools (`skill_diff` / `skill_update` / `skill_set_ownership`)** | `runtime/tools_registry.go` — all three `ToolStatusExcluded` in the matrix; actual handlers are CLI-runtime MCP tools in `clitools/mcp_tools_skill_governance_gaps.go` over the existing `/api/skills/*` routes, gated per `permguard/inventory.json` (`skill_diff` read-only `exempt`; `skill_update` `gate: via:PUT /api/skills/{id}`; `skill_set_ownership` `gate: via:PUT /api/skills/{id}/ownership`). The two write tools are also added to `DefaultInAppAdminDenylist` (`runtime/firtal_gateway_bridge.go`) alongside the other skill-governance writes. | FIR-1775 §5 — closes the CLI-only gaps in the skill-governance MCP surface (`multica skill diff/update/ownership` had no MCP wrapper; `skill_audit` was closed earlier in TECH-3077). Same posture as row 4c: matrix-`Excluded` entries are display/inventory-parity only — a write still requires passing the route's own permission gate, and `skill_update` bypasses only the change-request *review flow* (which its tool description warns about, steering agents to `skill_propose_change`), never the route auth. |
| 4e | **Workflow CLI/MCP tools (`list_workflows` / `get_workflow` / `create_workflow` / `update_workflow` / `delete_workflow` / `toggle_workflow` / `activate_workflow` / `get_active_workflow`)** | `runtime/tools_registry.go` — all eight are `ToolStatusExcluded` in the matrix; the actual CLI-runtime MCP handlers live in `clitools/mcp_tools_workflow.go` and call the existing `/api/cerebro/workflows*` routes with the caller's own identity. `permguard/inventory.json` classifies the three GET wrappers as read-only `exempt` and the five mutations as `gate: via:<route>`. | FIR-2937 — adds workflow management parity to the CLI/MCP surface without creating a new permission path. Matrix-`Excluded` means the eight entries are inventory/display metadata and are not native in-app gateway tools. When the optional full CLI/MCP bridge is enabled (row “Full Multica CLI/MCP tool surface”), handlers can be supplied behind the unchanged tool-policy list/call gates; every request still passes the workflow HTTP route's workspace/auth checks, so the bridge and the wrappers never bypass route authorization. |
| 4f | **Workflow Hook HTTP/CLI/MCP tools** (`list/get/create/update/test/publish/effective/runs`) | `platformcatalog` binds every concrete hook tool to `hooks:read`, `hooks:write`, `hooks:enforce`, or `hooks:manage_managed`. The key's contract in `platformaccess` is resolved only by `toolpolicy.ResolvePermission`, shared by the capability card, claim-time tool exposure, and `workflows/hook_toolpolicy_authorizer.go`; CLI and MCP wrappers call the same HTTP routes. | **FIR-3101/FIR-3388 — opt-in authority with truthful discovery.** A new agent is read-only. `hooks:write` is explicit and can only create a new `dry_run` version. `hooks:enforce` is human-only and required for `Publish`; `hooks:manage_managed` is workspace-owner-only. `manage_workflows` and tool registration grant none of these rights. The capability response separates `allowed`, `available`, `enforced`, `callable`, and `verified`; an ungranted mutation is omitted from the callable claim-time brief and denied by the same rule at call time. A typed action re-checks the policy creator's current `hooks:write` access when it executes, so revocation takes effect immediately and hooks cannot amplify their creator's authority. |
| 4h | **Workflow Command library tools** (`list_commands` / `get_command` / `create_command` / `update_command` / `delete_command`) | The shared handlers live in `clitools/mcp_tools_command.go`, call `/api/cerebro/commands*` with the task/member's own identity, and are bridged into both local MCP and the Firtal Gateway registry. `runtime/tools_registry.go` marks all five callable; `permguard/inventory.json` classifies reads as read-only and mutations as routed through their workspace-member HTTP endpoints. | **FIR-3493 — no independent authority.** The tool-policy list/call verdict controls whether an agent sees or invokes each tool, and the HTTP handler then re-checks authenticated workspace membership and scopes every row by `workspace_id`. Mutations can only change reusable command definitions in that workspace. Saved workflows execute their own copied `argv`, so later command edits or deletion do not rewrite an existing workflow. |
| 4g | **Eval catalog CLI/MCP tools** (`list_evals` / `get_eval` / `create_eval` / `update_eval` / `delete_eval` / `list_eval_runs` / `record_eval_run` / `list_eval_bindings` / `bind_eval` / `unbind_eval`) | Local MCP handlers live in `server/internal/cerebro/clitools/mcp_tools_eval.go`; CLI commands live under `multica workflow eval` in `server/cmd/multica/cerebro_eval.go`. Both call the same `/api/cerebro/evals*` routes. `runtime/tools_registry.go` mirrors every tool as `ToolStatusExcluded`, allowing the optional full CLI/MCP gateway bridge to expose the identical contract without a second implementation. | **FIR-3308 — no parallel permission path.** Reads use the authenticated workspace context. Every write is inventoried as `via:<route>` in `permguard/inventory.json` and therefore inherits the HTTP route's workspace-member gate. The wrappers do not grant eval or workflow authority themselves. |
| 4a | **`create_file` (agent-produced files)** | `runtime/firtal_gateway_create_file.go` (`FirtalCreateFileTool`), engine in `server/internal/cerebro/filegen`; registered in `firtal_gateway_tools_extended.go`, status `NewlyImplemented` in `tools_registry.go` as a gateway/runtime tool, not an MCP CLI tool | TECH-3416 Fase 2a. Lets a chat/gateway agent generate a file (md/txt/csv/json/html/svg/xml) from inline content and attach it to the **current chat or issue** — the target is pinned server-side from the task surface (`ChatMessageID`/`IssueID` on `ToolContext`) and is **never agent-chosen** — the input schema exposes only `filename`/`format`/`content`/`rows`, no target argument (surface-only least-privilege; `TestCreateFileSchema_NoModelControlledTarget` guards it). The handler makes **no permission decision of its own**: like every other tool it is exposed only when the runtime tool cascade (row 2) enables it, and is subject to the same `guardToolCall` chain. Every created file records origin metadata (`uploader_type=agent` + `uploader_id`, plus the chat-message/issue link). Per-file-type Allow/Ask/Deny gating (resource_pattern) is a planned follow-up that will ride the same chain as file reading. |
| 5 | **web_fetch host policy** | `runtime/firtal_gateway_tools_extended.go` (`WebFetchTool.Call`) resolves `server/internal/cerebro/webfetchpolicy` (`Effective`); per-workspace config in table `cerebro_web_fetch_policy`, admin CRUD at `GET`(member)/`PUT`(owner+admin) `/api/workspaces/{id}/web-fetch-policy` (`webfetchpolicy.Handler`, wired in `router.go`) | **TECH-3522 — per-workspace, admin-configurable.** A workspace admin picks a **mode** — allow-list (only listed hosts may be fetched) or disallow-list (everything except listed hosts) — and a list of host rules (`github.com`, `*.github.com`; bare host matches the apex + subdomains, `*.host` matches subdomains only, mirroring the legacy suffix logic). Resolved on **every** call; a host the policy does not permit is rejected **before** the HTTP request, with an error naming the active mode. A workspace with no row uses the seeded default — an allow-list of the legacy hardcoded hosts (`firtal.com`, `docs.anthropic.com`) — so behaviour is unchanged on deploy (no regression). Gated by feature flag `cerebro_web_fetch_policy` (**default ON**); with the flag OFF the gateway falls back to that same seeded default. The active list is injected into the agent's system prompt (`webFetchPolicyHint`) so the agent can explain to the user why a host was blocked. |
| 5a | **firtal_registry data-source access** | `runtime/firtal_gateway_tools_extended.go` (`FirtalRegistryTool`, `chainGateDataSource`) plus `toolpolicy.AppendRegistryDataSourceRows` | **Canonical policy only.** Data-source access for `list_data_sources`, `get_schema`, and `execute` resolves the full Workspace → Runtime → Agent → Group → User chain from `cerebro_tool_policy` on every call and fails closed on lookup errors. The gate uses `ResolveDeclared`, whose key classification keeps Registry access hard-floor, so a displayed effective decision and the actual data-source call cannot choose different resolution modes. Migration 9108 preserved the former allowlists as policy rows; migration 9147 then removed the direct per-agent store. The Permissions table crosses the live Registry catalog with those same authored resource rows for every actor scope, including Agent. Registry credentials and write authorization remain server-side, and read access never implies write access. |
| 5b | **Project visibility (cerebro_project_grant — additive layer, FIR-2125)** | `server/internal/cerebro/projectgrant/handler.go` (`requireAdminOrOwner`); `server/pkg/db/queries/project.sql` (`ListProjectsAccessibleToUser` + CEREBRO-PATCH(list-projects-grant-access)); `server/pkg/db/generated/project.sql.go`; migration `9106_cerebro_project_grant.up.sql` | **FIR-2125 — Collections hierarchical project access.** New `cerebro_project_grant` table adds a second access path alongside the existing `project.access` / `project_member` model. The two models are additive: a project is visible if EITHER the old model permits it OR the user has a grant (workspace / member / group) on the project or any ancestor project via `WITH RECURSIVE` over `project_nesting`. Write endpoints (`PUT`/`DELETE /api/cerebro/project-grants`) are protected: the caller must be a workspace `admin` or `owner` (checked via `member.role`). Grantee types: `workspace` (whole workspace), `member`, `group`, `agent`, `runtime`. Roles: `viewer`, `editor`, `full_access`. **Cutover strategy**: the old model (project.access / project_member / cerebro_project_group_member) stays live throughout rollout — new grants are additive, not replacing. A future migration will backfill old rows into `cerebro_project_grant` and deprecate `project_member`. **Conflict rule for effective role**: when a member has grants at multiple levels (direct + inherited from parent project), the highest role wins. The `ListProjectsAccessibleToUser` query only resolves visibility (yes/no); role-level resolution for the `ProjectGrantsPanel` UI happens client-side from the `view=effective` endpoint. UI is gated behind `cerebro_collections` feature flag (default OFF). |
| 6 | **Who can create/use a runtime, create/trigger an agent** | `server/internal/cerebro/grouppermissions/permissions.go` (`CanCreateRuntime`, `CanCreateAgent`, `CanUseRuntime`, `CanUseAgent`); enforced in `handler/group_permissions_cerebro.go` | Live group-permission checks on the relevant endpoints. Service always wired in `router.go`. |
| 6a | **Cognee-backed agent/member memory (`cerebro_memory` pilot)** | Workspace switch: `cerebro_memory` feature flag (`registry.ts`, **default OFF**). Capability: `server/internal/cerebro/grouppermissions/permissions.go` (`CapabilityCreateMemory` / `CanCreateMemory`), same shape as row 6's `CanCreateAgent` — **default deny**, granted per group on the group detail page ("Create memory" capability row). Per-user-per-agent toggle: `server/internal/cerebro/agentmemory.Service` (`GetSettings`/`SetSettings`), table `cerebro_user_agent_memory_settings` (migration 9110) — two independent switches, `can_read_memory` / `can_write_memory`, **both default OFF**, absence-of-row is the default-off state. The `create_memory` capability row itself is authored via migration 9109. HTTP route: `GET`/`PUT /api/agents/{id}/memory-settings` (`server/internal/handler/agent_memory_settings_cerebro.go`, human-only via `RequireUserScope`) — `GET` only requires the workspace flag (reading your own always-false-by-default switches is harmless); `PUT` additionally requires `create_memory` via `cerebroRequireCapability`, since flipping a switch on is "using memory". Settings UI: the per-agent "Memory" tab (`packages/cerebro-agent-memory/views/memory-tab.tsx`) mounted on the agent detail page, mirroring the existing Capabilities tab extension pattern. | **Three layers, all must pass for private (member/agent) memory:** (1) workspace flag on, (2) viewer's group has `create_memory`, (3) the viewer has flipped `can_read_memory`/`can_write_memory` for that specific agent. **Company memory has only layer 1** — readable by every agent once the workspace flag is on, no per-agent opt-in. "Private" means relevance-scoped to that user, not secret — an admin/owner can see all memory regardless of these switches. All three layers are now reachable by a user: workspace flag → group capability → per-agent toggle UI, in that order. A `PUT` with `cerebro_memory` off, or without `create_memory`, returns 404/403 respectively rather than a silently-ignored write. **Known gap (FIR-3117, tracked in FIR-3176):** layer 2 (`create_memory`) reads the OLD group-capability table (`grouppermissions`), not the unified tool-policy chain — so Memory access does not follow the member-override model (FIR-2175/FIR-3062) that the general tool-policy gate uses, and does not show up in `multica tool-policy explain`. Moving it onto the chain is in scope for FIR-3176, not this doc's "done" state. |
| 6b | **Memory tools (`memory_recall` / `memory_remember` / `memory_delete`) — the runtime half of 6a (FIR-1794)** | `server/internal/cerebro/runtime/memory_tools_cerebro.go` (`resolveCerebroMemoryGates`, `CerebroMemoryToolsForTask`, `stampScope`); offered additively in the gateway executor's `runToolLoop` (`runtime/firtal_gateway_executor.go`) and the external-runtime invoke path (`runtime/tool_executor_invoker.go`); reflected in `GET /api/agents/{id}/tools` (`handler/agent_tools.go`); schemas merged in `cmd/server/router.go` (`CerebroMemoryToolSchemas`) | Row 6a defines the three-gate permission model; this row is the enforcement when an agent actually calls the tools. **Offering:** `memory_recall` is offered when the workspace flag `cerebro_memory` is on (company memory is workspace-readable by design, per 6a); `memory_remember`/`memory_delete` additionally require the originating user's `create_memory` capability AND that user's `can_write_memory` switch for this agent. **Call time:** every `Call` re-resolves all gates via `resolveCerebroMemoryGates` — a manual grant row or a stale tool listing cannot bypass a switch that has since been flipped off. All error paths (lookup failure, missing row, missing user) **fail closed** to off. One deliberate tightening vs the HTTP layer in 6a: workspace admins are NOT auto-granted the capability on the machine path — a tool call takes the explicit group grant like everyone else. **Server-side identity stamping:** the input schemas expose no `subject_id`; `stampScope` derives it from task context — private = `user-{originating user}-agent-{agent}`, company = `workspace-{workspace}` — and a spoofed subject in the arguments is discarded (test-covered), so an agent can never read or write another subject's memory. The "originating user" is the task's original human (`OriginalUserID` / cascade user), not comment authorship; a run with no human origin (e.g. a system autopilot) gets no private memory at all. The three tools are `ToolStatusExcluded` in `tools_registry.go`, so they can never be enabled via `agent_tool_grant` — the memory gates are the only path. The tools proxy the internal cognee-memory-service; the service URL/credential stay server-side and never reach the agent. **Auto-recall (layer 3, same FIR):** at run start the server itself recalls memories relevant to the task (`runtime/memory_autorecall_cerebro.go`, gateway injection in `executeTask`, daemon claim field `MemoryContext` via `handler.applyMemoryAutoRecall`) and injects them into the agent's context. It resolves the SAME gates through the same `resolveCerebroMemoryGates`: company store needs only the workspace flag, the private store additionally needs the originating user's capability + `can_read_memory` switch, and the subject ids are stamped server-side — so auto-recall can never widen what the tools could read. Unlike the tools it fails OPEN **to "no injection"** (never to broader access): any gate/lookup/service failure means the run simply starts without recalled memories. |
| 7 | **Who can wake an agent via @mention** | `server/internal/cerebro/mentiongate/gate.go` (`CanTriggerMention` → `canTriggerAgent`); enforced in `handler/comment.go` | Checked on every comment that mentions an agent/squad leader. Disallowed mention is skipped (agent not triggered). Agent-authored mentions also need a task delegation context with an original human principal (`service.TaskService.CommentDelegationContext`). **TECH-3629:** when the running task carries no `original_user_id` and neither the issue creator nor the latest member comment yields one, the gate now follows the issue's `origin_type='agent_task'` pointer back up the creating-task chain (`server/internal/cerebro/delegationorigin.ResolveHumanViaOrigin`, the same source the "På vegne af" sidebar uses, walked transitively up to 10 hops) to recover the human who ultimately started the chain — closing the gap that blocked agent→agent handoffs on agent-created sub-issues. Only when **no** human is recoverable anywhere does the gate return the typed `delegationorigin.ErrMissingHuman` sentinel; the mention handler then routes that to a run-request to the **agent owner** (the owner of the agent being mentioned) via the existing `PrivateAgentRunRequester` flow (row 7a) instead of dropping it — so a truly human-less chain asks the agent owner to approve rather than dying. The owner-approval step is itself the loop-brake. The legacy "delegation blocked" system comment remains only as the fallback when no run-requester is wired. **FIR-2409:** the hardcoded rule (`baselineCanTrigger`: owners/admins master key, members limited to group-granted/owned agents) is now the **default**, layered with the configurable `trigger_other_agent` capability resolved through the tool-policy chain (workspace > group > member > agent). No stored rule (`DecidedBy==""`) → baseline applies unchanged (1:1). Explicit **Deny** removes the master key even for an owner/admin; explicit **Allow** grants a member the baseline blocks. Triggering your own agent never consults the policy. Base = Allow. |
| 7a | **Private agent run-request instead of direct wake** | `handler/comment.go` (`enqueueMentionedAgentTasks`, `requestPrivateAssigneeRunOnComment`) → `server/internal/cerebro/privateagentrun.Service.RequestPrivateAgentRun` | A member who tries to wake a private agent they cannot trigger does **not** get a task enqueued directly. Instead, when `cerebro_private_agent_requests` is on (default), the agent owner gets a `private_agent_run_request` inbox item. This covers both the explicit @agent/@squad mention path (FIR-2385) **and** the plain-comment `on_comment` path where the issue is already assigned to that private agent (**TECH-3252** — previously the on_comment gate returned false and the comment was silently dropped). Comments addressed to another member/squad/@all still suppress the assignee trigger. |
| 8 | **Sandbox profile (OS-level)** | `server/internal/cerebro/sandboxprofile/profile.go` (policy shape) + `server/internal/daemon/sandbox.go` (`buildSandboxConfig`) → `server/pkg/agent/sandbox` | Network mode, allowed hosts, writable/denied paths, shell deny, denied executables — enforced by the OS sandbox (macOS seatbelt / `sandbox-exec`) when the daemon launches the agent. Real kernel-level restriction. **FIR-1428 — the agent-browser unix-socket gate is wired live to the tool-policy chain.** agent-browser (a local CLI that drives a real Chrome over a Unix domain socket) is registered as a capability `tools:agent-browser` (via the `claude` provider tool list in `capabilities/discovery.go`, mirrored into `cerebro_capability`), so it appears in the Tools tab with Allow/Ask/Deny at every layer. At task claim `handler/daemon.go` (`ClaimTaskByRuntime` → `resolveAgentBrowserAllowed`) resolves that policy through the full Workspace › Runtime › Agent › Group › User chain with **Base = Deny** (unlike the generic table's Base=Allow — browser access is OFF until explicitly granted); only an explicit **allow/ask** stamps `allow_agent_browser` into the runtime sandbox policy sent to the daemon. `buildSandboxConfig` then sets `AllowUnixSocketBind` (emits `(allow network-bind/network-outbound (local unix-socket))` in the seatbelt profile) and adds `~/.agent-browser` to writable paths. Deny / no grant / resolve error keeps the socket sealed (fail closed), so agent-browser cannot start. |
| 8b | **Agent Vault login provisioning for agent-browser** | `handler/agent_browser_auth_cerebro.go` on `POST /api/cerebro/agent-browser/provision-auth` → `multica agent-browser provision-auth` → `agent-browser auth save --password-stdin`; deployed-app checks start with `POST /api/cerebro/agent-browser/internal-verify`, poll `GET /api/cerebro/agent-browser/internal-verify/{jobId}`, then write the returned screenshot via `multica agent-browser internal-verify --screenshot <path>` | **FIR-3006/FIR-3763.** Task-token-only and exact-box gated: the owner must belong to `browser-testers`, the exact calling agent must be on that same group's agent allowlist, the group must explicitly Allow `credential.reveal` on `agentvault-vault:Shared/browser-login/<app>`, and the complete Workspace › Runtime › Agent › Group › User chain must allow the same resource for the requested host. Each fixed target declares whether it needs a form login, Cloudflare Access headers, or both, and names the exact keys it may reveal. Access-only Atlas role checks read only their dedicated Admin or Reader Cloudflare pair; the unknown check reads no credential. Secrets stay in process memory and service-token headers travel only through `agent-browser batch` stdin; child output is suppressed. Browser checks run as bounded background jobs so cold starts cannot exceed Cloudflare's single-request limit; polling repeats authorization and restricts each job to its originating workspace and agent. The backend validates the post-login markers and captures a PNG before closing the browser session; the CLI writes that PNG with owner-only permissions and prints only its path plus non-sensitive verification metadata. Older CLIs retain the synchronous contract unless they explicitly request async mode. |
| 8a | **Personal browser (in-app, per-action feature + host gate)** (`tools:personal-browser`) | `server/internal/handler/personal_browser_authorize_cerebro.go` (`AuthorizePersonalBrowser`) on `POST /api/cerebro/personal-browser/authorize` → `personalBrowserFeatureEnabled` + `toolpolicy.Store.ResolvePermission`; the `agent_opt_in` contract lives in `platformaccess`; capability via the `claude` provider tool list in `capabilities/discovery.go` (mirrored to `cerebro_capability`); desktop transport in `apps/desktop/src/main/cerebro-browser-control-server.ts` | **FIR-2037/FIR-3320 — the personal browser is a Multica-owned Chromium pane in the desktop app that an agent drives (the user's logged-in session).** Unlike agent-browser (row 8), personal-browser is authorized **per action, per host**: the desktop control server calls the endpoint before every action with the target/current host and the agent's own `mat_` token. The server first resolves the effective `cerebro_browser` feature flag for the agent owner (locked workspace value → personal override → unlocked workspace default), then resolves `tools:personal-browser` as an agent opt-in with `RequestContext{Host}`. No Agent-layer Allow stays denied; an Agent Allow opens only when the host condition matches and no runtime/group/user/delegation/system ceiling blocks it. Workspace Disable remains a hard stop. Feature OFF, Ask, Deny, lookup errors, and authorization errors all fail closed. The desktop loopback transport starts even while the feature is off so the CLI can receive the precise feature-disabled verdict; the transport itself grants no access. The optional claim-time `MULTICA_PERSONAL_BROWSER` hint is not trusted because local daemons reserve and strip `MULTICA_*` custom environment keys. Every verdict is audited (server log + local machine log; never the typed value). |
| 9 | **Autopilot visibility / edit / trigger** | `server/internal/cerebro/access/autopilot_scope.go` (`CanSee`, `CanEdit`, `CanTrigger`); enforced in `handler/autopilot_cerebro.go` | Scope (workspace / personal / group) checked on every read/write/trigger. |
| 10 | **Repo checkout** (`repo.read` / `repo.checkout` / `repo.push`) | `handler/repo_approval_cerebro.go` (`CheckDaemonRepoCapability`) → `toolpolicy.Resolve`; called from `server/cmd/multica/cmd_repo.go` (`runRepoCheck`) | One of two things wired live to the tool-policy chain (see also row 11). Only Workspace + Agent layers carry signal for an autonomous checkout. Base = Allow (a repo with no policy is reachable; an admin tightens specific repos). "Ask" needs a human, which a daemon checkout has not, so only explicit Allow passes. |
| 11 | **Who can create a LOCAL runtime** (`create_local_runtime`) | `handler/runtime_setup.go` (`CreateRuntimeSetupToken`) on `POST /api/runtime-setup/tokens` and `POST /api/workspaces/{id}/runtime-setup-token` → `handler/group_permissions_cerebro.go` (`cerebroRequireLocalRuntimePolicy`) → `toolpolicy.Resolve`; catalog entry in `cerebro/platformcatalog/catalog.go` | **FIR-2672 — the second thing wired live to the tool-policy chain.** Consulted unconditionally at the local-runtime setup-token mint, independent of approval-inbox availability. Workspace + Group + User layers carry signal (groups auto-resolved from the user). Base = Allow, so nothing changes until an admin sets Deny on a group/member — preserving the prior behavior 1:1. Admins bypass. Anything other than Allow (Deny **or** Ask) blocks the mint, since the mint has no inbox-approval path yet. This is layered on top of the existing `create_runtime` group capability (row 6), which still gates the same mint. |
| 12 | **Agent comments must address a recipient** (FIR-2674 / TECH-3279 / FIR-1501) | `server/internal/cerebro/commentguard/guard.go` (`RejectComment`, `hasRecipient`); enforced in `handler/comment.go` (`CreateComment`, after `ExpandIssueIdentifiers`) | **Off by default** — gated by the `cerebro_comment_target_guard` feature flag (registry.ts), resolved per workspace at request time; turn it on from the Multica feature-flags screen (no env var, no restart). When on: an **agent**-authored comment that addresses no recipient is rejected with 422. A recipient is a member, another agent, a squad, or @all; a bare issue link (`mention://issue/...`) does NOT count — it points at a case, not a person (TECH-3279), and mentioning the authoring agent itself does NOT count either (FIR-1501). Member comments are never gated. Note: a compliant agent comment must therefore mention a member (notifies a human), mention another agent/squad (enqueues a run), or rely on the active-wakeup exemption when that feature flag is enabled — self-tagging is not a valid recipient. |
| 13 | **Agent wakeups / heartbeat scheduling** | `server/internal/cerebro/wakeup/handler.go` behind `/api/cerebro/wakeups`; CLI/MCP wrap the same endpoint. Routes sit inside `middleware.RequireWorkspaceMember` + user-scope group. Dispatch creates a system comment and enqueues through the existing task service. | Creating, listing, reading, and cancelling wakeups requires workspace membership. A wakeup can only target an issue and agent in the same workspace; archived agents are rejected. When the wakeup fires, dispatch reuses existing task enqueue gates such as workspace pause, agent-pass, runtime availability, and task queue notification. |
| 14 | **Who can administer workspace connections** (`manage_connections`) | `server/cmd/server/router.go` (the connection-write group uses `RequireWorkspaceMemberFromURL` + `h.RequireConnectionsManagePolicy("id")`) → `handler/group_permissions_cerebro.go` (`cerebroRequireConnectionsPolicy` → `RequireConnectionsManagePolicy`) → `toolpolicy.Resolve`; catalog entry in `cerebro/platformcatalog/catalog.go` (`manage_connections`) | **TECH-3513 slice 1 — the third thing wired live to the tool-policy chain.** Gates `POST /api/workspaces/{id}/connections`, `POST .../connections/test`, `PUT .../connections/{connId}`, `DELETE .../connections/{connId}`. Replaces the old hardcoded `RequireWorkspaceRoleFromURL("owner","admin")` on those routes. Consulted unconditionally, independent of approval-inbox availability. Workspace + Group + User layers carry signal (groups auto-resolved from the user). Base = Allow + admin bypass preserve the prior behavior 1:1 (owner/admin still pass with no stored rows); an admin tightens a group/member to restrict. Anything other than Allow (Deny **or** Ask) blocks, since these routes have no inbox-approval path yet. The connection *reads* (`GET .../connections`) and the runtime MCP execution endpoint (`POST .../mcp`) stay member-level — this gate is only "who may create/edit/delete connections". Distinct from row 2b, which gates which connection *tools* an agent may call. |
| 15 | **Who can copy workspace contents into another workspace** (workspace-copy) | `server/cmd/server/router.go` — `POST /api/workspaces/{id}/cerebro/copy` is mounted with `RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin")`; `cerebro/workspacecopy.Handler.Copy` then requires the same caller to be owner/admin of `target_workspace_id` before any copy branch can write. | **TECH-3582 / FIR-3820.** Owner/admin is required in both the source and target workspace. The copy is **non-destructive** — it creates new rows in the target workspace and never deletes or mutates the source. Missing target membership, a non-admin target role, and membership lookup failures all fail closed before any entity-specific operation. No tool-policy/approval path yet. |
| 16 | **Who can use the "Test as user" lookup** (`tools:test-as-user`) | `server/internal/cerebro/toolpolicy/handler_test_as_user.go` (`TestAsUser`, `TestAsUserAccess`) → `toolpolicy.Store.ResolvePermission`; the `human_opt_in` contract lives in `platformaccess`; routes `POST`/`GET /api/workspaces/{id}/cerebro/test-as-user[/access]` mounted member-level in `router.go` | **FIR-1771/FIR-3388.** The Test as user feature (resolve another user+agent's effective tool verdict from the profile menu) is an **OFF-by-default (opt-in) capability** gated in-handler for the **caller**, with **NO admin bypass** — being a workspace admin does not by itself grant it. The shared resolver grants only an explicit Allow at the **user or group** layer, lets an explicit user Deny revoke it, and denies agents. Everyone else gets 403 (`TestAsUser`) / `{"allowed": false}` (`TestAsUserAccess`). Migration `9099_cerebro_test_as_user` seeds the capability per workspace and a single user-layer Allow hardcoded to one specific user UUID (today, Jesper's) — not a role-based grant, so it does not follow ownership if it ever changes hands. The lookup itself reuses `Store.Table` (which auto-resolves the *target* user's groups), and Table applies the same declared contracts, so the in-app answer matches `tool-policy explain` and call-time enforcement. Read-only: it resolves and returns a verdict, mutating no state. |
| 17 | **`report_loop_check` (loop delivery-gate check reporting)** | `server/internal/cerebro/runtime/firtal_gateway_loop.go` (`FirtalReportLoopCheckTool`); registered in `firtal_gateway_tools_extended.go` only when `ToolContext.LoopStore` is wired (`main.go`: `gatewayExecutor.SetLoopStore(cerebroloops.NewStore(pool))`) | FIR-2283. Not gated by the tool-policy chain — presence is wiring-only (nil store ⇒ tool absent for every agent). The tool itself grants no access beyond recording one exit code: the worker agent reports the exit code of a check the engine already dispatched to it (`(issue_id, gate, round, argv)`, matched server-side), and `loops.Store.Report` only updates a matching pending row — an agent cannot invent a new check, target another issue's gate, or influence the pass/fail decision itself, since the gate (`loops.Reconcile`, consulted via `workflows.GateEvaluator`, row 2 shape) decides from the reported exit code alone, never the agent's judgement. |
| 17a | **`open_loop_step` (task-scoped workflow step creation)** | Capability derived from the trusted task context in `runtime/firtal_gateway_loop.go` (`loopStepCapabilityFromTask`); exposed by the in-app gateway in `firtal_gateway_executor.go` and by the task-token `/api/agents/{id}/tools` + invoke path in `handler/agent_tools.go` / `runtime/tool_executor_invoker.go`; persisted by `loops.Store.OpenNextStep` | **Not a general tool grant and not configurable through the tool-policy table.** The tool exists only for the agent task that represents a steps-enabled workflow block. The server, not the model, pins the issue, workflow, phase, block, current step, `Steps.Max`, and phase `MaxSteps`; the input schema exposes none of them. Missing/malformed task context, a task belonging to another agent, or a block without bounded steps leaves the tool unavailable. Repeated calls from one task return the same successor, while the first request beyond either authored limit durably fails the phase. This narrow task capability is the only path that bypasses the ordinary runtime-tool grant list. |
| 18 | **Who can manage Mode configuration** | `server/internal/cerebro/sessionmode/handler.go` (`requestContext`) on `PUT /api/cerebro/session-modes/{mode}/draft` and `POST .../{mode}/publish|restore` | FIR-3111. Any workspace member may read the active Mode profiles and version history. Only a workspace owner/admin authenticated as a human may edit, Publish or Restore; task-token actors are explicitly denied even when their originating member is an admin. This is a code-owned management gate, not a tool-policy permission. |

### Identity-aware API connection discovery (FIR-3290)

For an API connection with `auth_config.on_behalf_of.enabled`,
`APIConnectionResolver.ListForAgent` fetches the OpenAPI contract as the
authenticated agent (`X-On-Behalf-Of: agent:<uuid>`) before building the tool
list. The personalized contract is intersected with the connection's stored
endpoint catalogue, so remote discovery may remove or relabel an operation but
can never add one beyond the admin-authored ceiling. An operation marked
`x-registry-granted: false` is omitted. A missing extension keeps the operation,
preserving identity-blind APIs. The result is cached for one minute per
connection version and agent. A fetch/parse/auth error drops the whole
identity-aware connection from that list (fail closed); there is no fallback to
the shared static snapshot. The existing Allow/Ask/Deny resolver still runs
after this filter, and the call-time gate remains authoritative.

When at least one endpoint on that identity-aware connection survives policy,
the resolver also adds one derived `<connection>__discover_access` tool. Its
read-only payload comes from the same authenticated personalized contract and
lists the full remote surface, each remote grant marker, whether the concrete
endpoint tool is listed after Multica policy, and the access-request schema
tool when the API advertises one. It makes ungranted resources discoverable
without handing the model dead execution tools. The discovery tool anchors its
call-time identity and verdict to an already-admitted endpoint; if policy
admits nothing, it is absent and cannot become a bypass.

---

### Blocking eval gates on Issue workflows

The set_blocking_gate group capability is an always-on, default-deny permission
for binding an eval to an Issue workflow with enforcement=block. Workspace
owners/admins bypass the capability check. Other members must belong to a group
granted set_blocking_gate through the group detail capability control. Warn-only
bindings do not require this capability and never block workflow progress. The
check is implemented by
server/internal/cerebro/grouppermissions.Service.CanSetBlockingGate, with the
capability name validated against the cerebro_group_capability contract.

This is separate from the runtime tool registry inventory. The registry may
list eval and eval-schedule MCP tools, but their handler-level authorization and
the blocking-binding capability check remain authoritative; adding a tool to the
registry does not grant permission to create a blocking gate.

### Dead failed-run reads are issue-access gated (FIR-3901)

The two endpoints behind the red failed-run bar and the red inbox pip return
issue content — the failure reason, the raw error text and the name of the
machine that ran it — so they carry the same access floor as reading the issue
itself.

- `GET /api/issues/{id}/failed-runs` resolves the id through
  `Handler.CerebroIssueAccessGate`, which delegates to `loadIssueForUser`:
  workspace scoping first, then `canAccessIssue` (project access, standalone
  privacy, channel/DM subscriber membership). A caller who cannot read the
  issue gets 404, not 403 — the same existence-hiding rule as every other
  issue endpoint.
- `GET /api/inbox/failed-issue-tasks` is workspace-scoped in SQL, which is not
  the same as visible-scoped: a private issue or a channel the member is not in
  is still inside the workspace. `Handler.CerebroVisibleIssuesFilter` narrows
  the rows to the ones `canAccessIssue` allows before the response is written.

Both gates are injected into `server/internal/cerebro/inbox` by the router,
because that package cannot reach the unexported loader. Both **fail closed**:
an unwired gate answers 404 / an empty list rather than falling back to
"any authenticated user may read".

Before FIR-3901's follow-up the per-issue endpoint checked only that a user was
signed in and looked the run up by raw UUID, so knowing an issue id was enough
to read its failure text from any workspace. The regression cases are pinned in
`server/cmd/server/dead_failed_runs_integration_cerebro_test.go`.

## What is OFF by default (and only this)

These exist and work, but with default flags they block nothing for a normal
agent. Flipping the flag turns them on.

| System | Off-switch | Default | What turning it on does |
|---|---|---|---|
| **Policy Decision Service for Firtal Gateway tool calls** | always on for Firtal Gateway | **fail closed** | Every Gateway tool list and call resolves through the canonical policy plus presence in the live per-task Gateway registry. Unknown identity, an absent callable, Ask, Deny, lookup error, or missing service returns Deny. The separate availability card still requires two-sided `verified` evidence before it presents a capability as proven reality; that reporting threshold is not an authorization input. There is no runtime-tool cascade, `agent_tool_grant`, or hardcoded POC fallback in the Gateway. Connection, API-endpoint, credential, sandbox, repo, and `create_issue` safety floors remain additional ceilings. |
| **Old capability resolver + grants** (`approval_required` → Ask) | no live switch | **retired** | Gateway calls resolve through the Policy Decision Service. The approval gate remains only as the shared inbox/wait seam for Ask outcomes. |
| **Local-runtime per-tool enforcement** (Claude/Codex/Cursor/Gemini) | `ResolveDaemonToolPolicy` plus provider-native before-call seams | **always enforced** | FIR-3401 / FIR-3723 / FIR-3753 — every daemon-spawned local runtime resolves built-in and direct MCP calls through the same canonical tool-policy chain and approval inbox as Gateway. Claude and Codex use wildcard `PreToolUse`, Gemini uses `BeforeTool`, and Cursor uses fail-closed `preToolUse`. Codex starts with hooks explicitly enabled, the daemon-vetted task-local hook trusted, and the enable flag last so agent settings cannot disable the gate. Direct MCP calls keep their provider identity `mcp__<server>__<tool>` through call-time resolution. When the connection resolver admits an entire raw MCP server as Allow-only, the immutable task mandate snapshots the server-scoped `mcp__<server>__*` capability as well as currently discovered exact tools. This matches the raw relay's whole live `tools/list` surface even when the stored discovery manifest is stale, without widening to another server; Deny and mixed-verdict relay-withheld connections receive no wildcard. The task-scoped Capabilities card applies this same mandate to both top-level tools and `connections[].tools`, so `callable` reflects the running task rather than policy alone. Server-dispatched workspace Connection endpoints are the exception: local hooks wrap their shared `<connection>__<operation>` dispatch identity as `mcp__multica__<connection>__<operation>`, so the mandate gate removes only that wrapper before its exact snapshot check while policy resolution keeps the provider-native key. Cursor hook names (`Shell`, `Read`, `Write`, `StrReplace`, `Delete`, `Grep`, `Glob`, `WebSearch`) and their resource arguments are normalized server-side to the Cursor runtime inventory before both the immutable task-mandate check and policy resolution, so older daemons receive the fix without a protocol update and resource-specific rules still apply. Deny blocks; Ask waits for a final approval decision; resolver or transport failure blocks. Runtime reports are authoritative for built-in inventory; claim-injected connection servers are authoritative through the unified connection resolver. The runnable local-provider list comes from the complete-before-call adapter registry; providers without a complete adapter are rejected before spawn and cannot run outside the engine. The former rollout switches and claim-time stage are deleted, so no local runtime can silently spawn without enforcement. Credentials, sandbox and repo floors remain independent and unchanged. |
| **Tool-policy UI table** | canonical runtime access editor | **always on** | The Runtime settings page always shows the rich effective-chain table; the former feature-off grant editor and its authoring API were removed in FIR-3403. |
| **Platform actions in the tool-policy table** | no secondary rollout flag | **always visible with Unified tool permissions** | The Settings API always includes the code-owned platform catalog (`platformcatalog`, incl. `create_local_runtime`). Engine-owned rows are settable and resolve through the same declared contract as call time. Actions protected by a different security boundary are read-only and name that external owner. The retired `cerebro_platform_capabilities` flag can no longer make a live permission disappear from Settings. |
| **Agent-start guidance** | feature flag `cerebro_agent_trigger_permissions` | **false** | Controls only the additional friendly Agent-start guidance tab. The underlying platform rows are already present in the canonical Permissions table regardless of this presentation flag, so the flag cannot alter or hide enforcement. |
| **Repo checkout/push routed through the tool-policy chain** | workspace setting `repo_grants_enabled` (daemon reads `Daemon.repoGrantsEnabled`, `server/internal/daemon/daemon.go`) | **off** | **FIR-1609 / FIR-2512 / FIR-2505 — flag-gated, behaviour-preserving while OFF.** When on, an autonomous `multica repo checkout/push/read` resolves through the tool-policy chain via `POST /api/daemon/workspaces/{id}/repo/check` (`handler.CheckDaemonRepoCapability` → `toolpolicy.Resolve`) instead of the flat workspace repo allowlist. Only the Workspace-root and Agent layers carry signal (an autonomous checkout has no human / runtime / group / user). Base = **Allow**, so a repo with no explicit policy stays reachable (1:1 with the legacy allowlist) and an admin tightens specific repos in the tool-policy screen; Deny blocks; Ask raises one shared-inbox approval and the real checkout waits for a human (`awaitRepoApproval`, FIR-2586) — the read-only `repo check` pre-flight only reports the decision. Action-scoped Conditions bite here: the request verb is derived into `RequestContext.Action` (`repo.checkout` / `repo.push`) so an action-list Condition on a rule applies. Off (default) = the flat allowlist path is unchanged. Before flipping a workspace ON, the **enablement-readiness audit** (`toolpolicy/enablement_audit.go`) reports the lock-out delta — the legacy-allowed grants the chain would Deny/Ask; an empty delta = a safe 1:1 flip. |
| **~~Per-data-source `firtal_registry` access routed through the tool-policy chain~~ (RETIRED FIR-2208)** | ~~workspace setting `registry_grants_enabled`~~ | **always on** | **FIR-2208 — this flag and the `RegistryGrantsEnabled` field are RETIRED.** `chainGateDataSource` now runs unconditionally whenever a chain resolver is wired (which is always the case in production). The flag-gated "only tighten after the allowlist" design is gone; the chain IS the enforcer. The legacy per-agent allowlist (`allowed_data_sources` / `allowed_data_sources_all`) is also retired — see row 5a. **Conditional rules (the WHEN layer) are unchanged and still live:** the gate threads `Action` = the registry verb into `RequestContext` so action-scoped conditions and (when `cerebro_policy_cel` is on) CEL conditions on data-source rows still evaluate here. The `chainGateDataSource` gate is a no-op when no chain resolver is wired (test fixtures, unchanged). |
| **Expression (CEL) conditions on tool-policy rules** | feature flag `cerebro_policy_cel` (`toolpolicy.FlagPolicyCEL`) | **off** | **FIR-1609 — flag-gated, fails closed while OFF.** Lets a per-tool rule carry a CEL expression as its WHEN condition, for genuine dynamics the structured terms (host allowlist, action list) can't express (e.g. "only during business hours"). Off: only the structured conditions apply and a rule that carries an expression stays **inert and fails closed** — it never silently allows. On: the CEL evaluator (`server/internal/cerebro/toolpolicy/cel.go`) is wired into the daemon/local-runtime gate, the repo gate, the credential cap gate and the registry data-source gate. No restart needed. |
| **Conditional rule semantics — Allow is a whitelist, Deny/Ask is a restriction** | `toolpolicy.ConditionedSetting` (`server/internal/cerebro/toolpolicy/condition.go`) | always (only affects rows that carry a condition) | **FIR-1609 — how an UNMET WHEN condition resolves, by Effect direction.** A conditioned **Allow** is a *whitelist grant* ("allow ONLY when the condition holds"): when the condition is **not met** (or is undecidable) the rule **denies** — it does not drop back to the base default. This is what makes a "conditional yes" real: because every gate runs `Base=Allow`, an unmet Allow that merely dropped would be inert (the resource stays allowed) — the QA-found bug where `Allow WHEN action=="get_schema"` still let `execute` through. A conditioned **Deny/Ask** is a *restriction* ("apply the restriction when the condition holds"): when its condition is **not met** the row **drops** (its layer inherits, as if absent); undecidable keeps the restriction. Unconditioned rows are unaffected (apply unchanged), and there are zero conditioned rows in any live workspace, so this is behaviour-preserving for existing policy. |

| **Per-permission-type WHEN condition kinds (read model)** | `toolpolicy.EnforcedConditionKinds` (`server/internal/cerebro/toolpolicy/condition.go`); surfaced as `enforced_conditions` on each table row (`toolpolicy/handler.go`); rendered by `packages/cerebro-tool-policy` (`condition-facets.ts` / `ConditionControl`) | always (read/UI only — no enforcement change) | **FIR-1708 C — read-model + editor, not a new gate.** Each table row now reports which WHEN kinds actually bite for that capability — `action` for the verbed keys (`repo.*`/`credential.*`, `ActionOf`) and the registry tool/its data-source rows; `host` for `web_fetch`; `cel` (the escape hatch) on every chain-gated row; **nothing** for `managed_externally` rows. Derived from the SAME facts the gates thread into `RequestContext`, so the set can't drift from what enforcement evaluates. The editor renders only the reported kinds, so a structured term that can't match (a host allowlist on a notification tool, an action list on a generic tool) is never offered, while WHEN stays available everywhere via CEL. The field is optional on the wire: an older backend that omits it makes the editor fall back to its prior client-side heuristic (no regression). **This changes only what the editor offers and the read model reports — resolution + enforcement (`ConditionedSetting`, the gates) are unchanged.** Repo and connection per-resource rows also now read back their stored `conditions` column (`table_repo.go`/`table_connection.go`), so an action condition saved on a repo/connection rule is visible in the table (FIR-1708 D) — previously the loaders never selected it. |

| **Per-permission usage log (read model)** | `toolpolicy.Store.RecordUsage` → `cerebro_tool_policy_usage` (migration 9132); read `GET /api/workspaces/{id}/tool-policy/usage?tool_key=...` (`toolpolicy/handler.go` `Usage`, admin/owner-gated); rendered as the "Usage" tab of the per-permission detail page (`packages/cerebro-tool-policy`, behind default-OFF `cerebro_permission_detail`) | always (write is best-effort observability — no enforcement change) | **FIR-3091 punkt 8 fase 3 — an audit trail of applied decisions, not a new gate.** Every LIVE enforcement point now appends one row when it applies a permission decision: the mention trigger gate (`mentiongate`, incl. baseline-decided calls with `decided_by=""`), repo checkout (`CheckDaemonRepoCapability`), the agent-browser sandbox gate, the gateway tool-policy gate (`guardToolCallViaPolicy`) and its connection verdicts (under the `connection:<name>` key), the local-CLI gate (enforce stage only — observe is a dry run and records nothing), and the member HTTP-action gates (`create_local_runtime` / `manage_connections` / `manage_credential_access`; admin-bypass paths record nothing because the permission is not consulted). Each row: enforcement point, subject (agent/member/system), concrete resource, applied decision (allow/ask/deny; a Disable verdict records as the deny it acts as), the deciding layer, timestamp. **Recording is best-effort AFTER the decision** — an insert failure is a log gap, never a blocked or altered action — so resolution + enforcement are byte-for-byte unchanged. History starts the day this shipped; the read is capped at 200 rows, newest first. |

| **Argument-value allowlist WHEN condition — data-source scoping** | connection `scopable_args` config (the picker); no separate flag — gate runs unconditionally (FIR-2208 retired `registry_grants_enabled`) | gate **always on**; structured term inert until a rule carries it | **FIR-2083 — a fourth structured WHEN kind beside host/action/CEL.** `Condition.ArgAllowlist` (`toolpolicy/condition.go`) restricts a named tool-call argument to an allowlist of values: `{arg, values}` entries AND together and a request value must equal one (case/space-insensitive); an absent or blank argument **fails the term closed** independently of whether the allowlist contains `""`. It is the structured form of "this rule applies only when `data_source_id` ∈ {a chosen set}", so **one rule whitelists a set of sources** instead of one Allow row per source. The `firtal_registry` gate threads the call's `data_source_id` into `RequestContext.ArgValues` and resolves **both** the per-source row and the set-covering rule (the source's `folder_id` is also threaded, so a rule conditioned on the folder auto-covers any source later added to that folder — no rule edit). A call to a non-selected source fails closed to Deny via `ConditionedSetting` (a conditioned Allow is a whitelist, row 90), so scoping bites; with no such rule, behavior is unchanged (base Allow). `EnforcedConditionKinds` reports the `arg` kind on `firtal_registry`/data-source rows so the editor offers the picker only where it bites. **Connection side:** a connection declares the scoping axis in its `scopable_args` JSONB (`{tool, arg, options_source_tool, group_by, tag_field}`; empty = unchanged); `GET /api/workspaces/{id}/connections/{connId}/options?tool=<declared options source>` relays that tool through MCP and returns the normalized `{id,name,folder,tags}` choices for the multi-select. The endpoint only calls a tool the connection has **declared** as an options source (`Connection.OptionsSourceFor`), never an arbitrary tool through the connection's credentials. MCP `Test & discover` also reads tool input schemas and returns an advisory `scope_suggestions` list when a string `*_id` argument has exactly one matching list/search/browse tool. The connection editor requires an admin to accept and save each suggestion; discovery never persists a scope or changes Allow/Ask/Deny by itself. Code: `toolpolicy/condition.go`, `runtime/firtal_gateway_tools_extended.go`, `connections/{store,handler,options,test}.go`. |

Issue recurrence write/run routes (`PUT`/`DELETE /api/cerebro/issues/{issueID}/recurrence/` and `POST /api/cerebro/issue-recurrences/{id}/run`) are classified under the `manage_issue_recurrence` platform capability. This is catalog coverage for the platform-actions table; it does not add a new runtime gate by itself.

Eval ownership mutations resolve the actor through the shared validated task/member resolver; client-supplied `X-Agent-ID` alone is never trusted. Blocking eval bindings fail closed when their `set_blocking_gate` authorizer is unavailable, and deleting an eval cannot cascade-delete a blocking binding unless the same permission check succeeds.
| **Agent Vault access-table reconcile** | feature flag `cerebro_agent_vault` | **false** | TECH-3196 / FIR-2210 — at task claim, if the workspace flag is on, the broker reconciles the admin-controlled access table `cerebro_agentvault_agent_access` (agent → {vault: role}) from the authoritative tool-policy credential grants (FIR-1739 Part B), so the table is a derived view of the chain (deny-by-default), not an independent authority. The table is admin-write only (an agent cannot grant itself a box). **FIR-2210:** the agent-side forward-proxy transport was REMOVED — the broker no longer injects `HTTPS_PROXY`/CA into `custom_env` and the standalone relay listener (`AGENT_VAULT_RELAY_LISTEN`) is gone. Agents reach credentials through Multica connections (the FIR-2166 server-side connection proxy, where the backend injects the credential and the agent never sees it), never a tunnel and never the internal Agent Vault path directly. `PrepareSpawnEnv` therefore returns a nil env today; the hook stays on the claim path as the reconcile trigger. Off (default) = no reconcile (claim unchanged). **Fail-open:** any reconcile error spawns the agent WITHOUT broker env, never blocking the claim. Code: `server/internal/cerebro/agentvault`; claim hook `handler.mergeAgentVaultEnvForClaim`. |
| **Full Multica CLI/MCP tool surface bridged into the in-app gateway runtime** | `runtime.MaybeEnableInProcessBridge` wired unconditionally at server startup | **on**; exposure remains permission-controlled | **FIR-1449 — supplies handlers, never widens exposure.** Each gateway per-task tool build (`runToolLoop`) mints a short-lived task-scoped `mat_` token and bridges the `clitools.RegisterTools` surface into the registry through an in-process loopback (`InProcessAPIClient`) carrying the task's own identity, so server-side authorization is decided exactly as for a remote CLI call. Admin-mutation tools are still dropped by `DefaultInAppAdminDenylist`, and every exposed tool remains subject to the authoritative Policy Decision Service. Code: `runtime/firtal_gateway_bridge.go`, `runtime/firtal_gateway_inproc_bridge.go`, `runtime/firtal_gateway_executor.go`. |
| **Cloudflare Access stamp as an accepted auth credential** | env `CEREBRO_CF_ACCESS_TEAM_DOMAIN` + `CEREBRO_CF_ACCESS_AUD` (server); per-machine `CEREBRO_CF_ACCESS_CLIENT_ID`/`_SECRET` or profile `cf_access_client_id`/`_secret` (client) | **off** (verifier is nil until both server env vars are set) | TECH-3396 — puts `multica-api.firtal.com` behind Cloudflare Access. **This is an authentication path, not a tool-policy gate.** When configured, `middleware.Auth` accepts a request carrying no Multica token if it has a valid `Cf-Access-Jwt-Assertion` (Cloudflare's edge stamp): the JWT is verified against the team's JWKS + AUD and its email is mapped to a Multica user (`server/internal/cerebro/cfaccess`). It is **additive** — existing `mat_`/`mcn_`/`mul_`/JWT/cookie paths are unchanged, so it never locks anyone out of an environment that is not behind the wall. The "unknown machine is rejected" guarantee is enforced by Cloudflare at the edge (before Multica), not by this code. Clients (CLI, daemon HTTP, wakeup websocket) attach their per-machine `CF-Access-Client-Id`/`-Secret` so they pass the wall; those headers are consumed by Cloudflare and never reach Multica. All header/verify logic is a no-op when unconfigured. |

**Firtal Gateway runtime timeout rules (FIR-2803).** These limits are liveness
and cost controls, not permission grants. `TaskTimeout`
(`MULTICA_SERVER_FIRTAL_GATEWAY_TASK_TIMEOUT`) is the optional whole-run
wall-clock cap; the default `0` means no whole-run cap. `CallTimeout`
(`MULTICA_SERVER_FIRTAL_GATEWAY_CALL_TIMEOUT`) bounds one gateway model call and
defaults to 10 minutes. `ToolCallTimeout`
(`MULTICA_SERVER_FIRTAL_GATEWAY_TOOL_CALL_TIMEOUT`) bounds one server-side tool
dispatch and defaults to 30 minutes. An uncapped whole run still cannot stall
forever because every model call/tool dispatch is individually bounded and the
tool loop is bounded by `MaxToolRounds`
(`MULTICA_SERVER_FIRTAL_GATEWAY_MAX_TOOL_ITERATIONS`, default 8) plus one final
no-tool model call. These settings never widen what an agent may reach: tool
exposure and call-time gates remain the rows above, especially rows 2, 2b, 3,
and 5a.

Customer-service MCP proxy inventory note: a permission grant cannot expose a
tool that the Firtal Gateway runtime never registers. When the customer-service
MCP surface adds an endpoint that should be callable by gateway agents, add the
matching proxy entry in `server/internal/cerebro/runtime/customer_service_mcp_tools.go`
and cover the name in `TestCustomerServiceMCPToolsAreRegisteredAndInMetadata`.

**FIR-2243 B1 — audit trace, NOT a gate.** `guardToolCall` (`runtime/approval_gate.go`, `logToolDecision`) now emits one structured `tool_call_decision` log line per tool call — the tool, the resolved decision label (`allow_gate_off` / `deny_connection` / `ask_connection` / `tool_policy` / `allow_ungated` / `capability_<outcome>` / `capability_error` / …), the `allowed` flag, the deciding `connection` when a connection rule decided it, and the run identity (`agent_id` / `agent_name` / `task_id` / `issue_id` / `surface`). It fires for **every** call, including the default-off allow path that previously logged nothing, so the runtime audit trail can answer "which agent ran which tool, on which issue, and was it allowed". This is **observability only**: it runs (via a deferred call on named returns) after the verdict is decided and never changes any allow/deny outcome. Its field names match the B2 gateway-trace line (`runtime/api_connection_tools.go`, `logGatewayCall`) so both can be consumed consistently.

**FIR-3399/FIR-3400/FIR-3830 — Decision Ledger and enforcement.** The Policy Decision Service resolves the concrete callable from the live per-task Gateway registry to the canonical capability catalog and fails closed when identity, live presence, or policy is uncertain. `guardToolCall` uses that result as the authoritative Gateway verdict and appends it to the immutable `cerebro_access_decision_ledger`; local-runtime Task Mandate rejections append the same diagnostic observation. Capabilities `observed_access` and the drift watcher combine ordinary tool messages with those mandate-denial rows, so a tool that general Permissions allows but the frozen task rejects is visible as blocked drift. A ledger write failure never changes the verdict. The availability card's stronger `verified` label remains a reporting claim that requires two-sided probe proof and is deliberately separate from authorization.

Loop-gate reporting tools: `report_loop_check`, `report_loop_judge`, and
`report_loop_human` are native Firtal Gateway tools registered only when a loop
store is wired into the task context (`runtime/firtal_gateway_tools_extended.go`).
They do not bypass the tool exposure model in row 2: an agent can call them only
if the legacy grant cascade or the tool-policy chain exposes the tool for that
run. The tools only record a verdict for an existing
`(issue_id, gate, round, check)` row: `report_loop_check` records the
programmatic exit code, `report_loop_judge` records the judge's structured
`pass` plus `blocking_issues` verdict, and `report_loop_human`
(`runtime/firtal_gateway_human.go`, `FirtalReportLoopHumanTool`) records an
explicit approve/reject decision for a human-type check — an agent assignee
standing in for a person's sign-off, not scoring a rubric (`approved` boolean;
a `note` is required whenever `approved` is false). A failing judge verdict must
include concrete blocking issues; a passing judge verdict stores an empty issue
list. When a **person** (not an agent) is the assignee of a human-type check,
they sign off through the `POST /api/cerebro/workflows/{id}/human-checks/{checkId}/approve`
route (FIR-2283, `workflows/handler.go` `ApproveHumanCheck`, listed in
`permguard`/`platformcatalog`) instead of the tool — the route records the same
decision on the matching pending row. Like their siblings, `report_loop_human`
and the approve route do not grant issue access, reveal credentials, or create
new work on their own; each only updates a matching pending check the engine
already dispatched.

**API-connection tool argument shape (FIR-2441).** An `api`-type connection tool
(exposed under the default-off `cerebro_api_connection_tools` flag, row 2b) takes
a fixed argument shape: **path** parameters at the **top level**, **query**
parameters inside a **`query`** object, and the request **body** inside
**`body`**. Passing query parameters at the top level instead of inside `query`
silently drops them and the call fails. This shape is stated to the agent up
front in its first prompt — both the local brief
(`server/internal/daemon/execenv/cerebro_tools_brief.go`) and the cloud gateway
system prompt (`runtime/firtal_gateway_executor.go`) — from ONE shared constant,
`connmeta.APIConnectionArgHint` (`server/internal/cerebro/connmeta/argform.go`),
re-exported as `runtime.APIConnectionArgHint`, so the local and cloud prompts
cannot drift on it (guarded by tests on both surfaces). See
`docs/agents/agent-tool-brief.md` for the render path. When an agent has **zero**
connection tools, the same brief now states that explicitly and points at the
read-only `multica_connection_tools_status` diagnostic, so silence is never
mistaken for a docs/tools load failure. This is documentation of an existing
tool's argument contract, not a permission gate.

Relevant files: `runtime/approval_gate.go` (`approvalGateEnvEnabled`,
`BuildApprovalGate`, `toolCapabilityKey`, `logToolDecision`),
`permissions/decision.go`, `permgate/permgate.go`, and
`packages/cerebro-feature-flags/registry.ts`.

The legacy grant resolver and its empty SQL read are retired. Credentials keep
the same fail-closed posture through a direct Deny floor plus the canonical
tool-policy chain; `permgate` only materialises canonical Ask decisions.

---

## Axes that are NOT tool/access control (don't fold these into tool-policy)

These constrain agents but along a different axis than "is this action allowed":

- **agentpass** (`agentpass/agentpass.go`) — spend/budget cap per issue (money, not access).
- **Budget block** (`runtime/firtal_gateway_executor.go`) — token budget.
- **auto-pause / ratelimit / codexlimit** (`runtime/auto_pause.go`, `ratelimit/`, `codexlimit/`) — resilience against provider rate limits.
- **permguard** (`permguard/permguard.go`) — test-time inventory / regression guard, not a runtime gate.
- **Infisical secret folders** (`infisical/client.go`) — enforced by the external Infisical ACL; can only be mirrored, not moved here.

---

## Known enforcement outage: 2026-06-23 → FIR-2743 restore

Upstream syncs #4454 (`handler/daemon.go`, June 23) and #4530 (`daemon/daemon.go`,
June 24) silently deleted several of the wirings this doc describes. Until the
FIR-2743 restore landed, the following were documented-but-dark:

- **Staged local tool-policy (TECH-2563):** the claim did not return
  `local_tool_policy_stage` and the daemon did not wire the Claude PreToolUse
  hook — observe/enforce workspaces got no local per-tool enforcement.
- **Workspace capability policy at claim (FIR-458):** sandbox network
  allowlists and per-agent MCP denies from Settings were not applied.
- **Capability register mirrors (FIR-2129/FIR-2284):** runtime snapshots
  stopped landing in the register on register/heartbeat, so the unified tool
  table went stale for runtimes that reconnected in the window.
- **OS sandbox at spawn (JEH-418/FIR-1428):** the daemon spawned agents
  WITHOUT the Seatbelt wrapper (no `Sandbox:` on `agent.New`), and the
  agent-browser gate folded its grant into a sandbox policy the daemon never
  received. Deny-by-default posture was effectively off for local runs in the
  window.
- **User communication profile (JEH-304):** claim-time compilation and the
  brief section were dropped (delivery feature, not enforcement — listed for
  completeness).

All of the above are restored and now guarded by source-presence tripwire tests
(`handler/daemon_permission_wiring_cerebro_test.go`,
`daemon/spawn_permission_wiring_cerebro_test.go`) so the next upstream sync
cannot drop them silently. If you are auditing behavior inside the outage
window, assume these gates were NOT enforced on local daemon runtimes.

## If you are mapping or refactoring permissions

- Start from the live table above; it is the source of truth for current behavior.
- Before claiming "X is unguarded," find the runtime call site and confirm it is
  not one of the live gates. Reading a struct definition is not enough — trace
  the call.
- When consolidating gates into the tool-policy chain, keep the deny-by-default
  posture of credentials and the OS sandbox intact. Prove the new path denies
  what the old path denied before removing the old path.

## Recovering direct grants after migration 9152

The Cerebro permission recovery command is the only supported recovery path for direct
`agent_tool_grant` rows already removed by migration 9152. It reads a separate
point-in-time-restored database in PostgreSQL read-only mode and compares it
with the current database. Dry-run is the default:

```sh
RECOVERY_SOURCE_DATABASE_URL='<restored database>' DATABASE_URL='<current database>' \
  go run ./internal/cerebro/grantrecovery/cmd/recovery --workspace-id '<workspace UUID>'
```

The JSON diff separates `mapped`, `already_present`, `conflicting`, and
`unmapped`. Non-Registry tool config, a missing target agent, or a conflicting
current policy blocks import. Never edit or reuse the restored database as the
target. After a reviewed zero-conflict diff and explicit production approval,
create a single-use approval request whose capability is
`permission_recovery:apply`, whose resource is
`workspace:<workspace UUID>:source:<source_fingerprint>`, and whose context
contains the FIR-3388 issue ID plus the
`production_permission_recovery_import` approval boundary. Jesper must approve
that exact request through the normal Approval inbox. Then repeat with
`--apply --approval-id '<approval UUID>'`. The command atomically verifies that
the approval is live, unused, belongs to this workspace and exact diff, and was
decided by a workspace owner or admin. The import uses a serializable transaction, takes a
workspace advisory lock, refuses races, and records a source fingerprint and
the imported identities in `cerebro_permission_recovery_audit`. A second run is
a no-op for identical rows. Delete the paid restored database only after the
post-import effective-decision comparison is complete.

## Keeping this doc honest (enforced)

A CI check (`.github/workflows/cerebro-permission-doc-guard.yml`) fails any PR
that changes permission/access-enforcement code without updating this file. If a
change genuinely does not affect documented behavior (a comment, a test, a
rename), add the label `permission-doc-not-needed` to the PR or put
`[skip-permission-doc]` in the PR description — a conscious skip, not a silent
one. This is why the doc stays true to the code instead of rotting.
# Embedded Chat on-behalf-of identity (FIR-2835)

Embedded applications authenticate two principals. The Bearer PAT authenticates the application; `X-Multica-User-Assertion` authenticates the person. The server sends the assertion to the configured identity provider's authoritative user endpoint, which verifies signature and expiry, then requires the configured upstream provider, maps the verified email to a workspace member, and replaces the request identity only after that mapping succeeds. This avoids copying a provider's JWT signing secret into Multica when asymmetric JWKS keys are unavailable.

Embedded requests cannot submit a Multica user ID, tool list, connection ID, or permission override. Chat tasks therefore inherit the verified member as `original_user_id` and continue through the normal five-layer tool-policy, runtime-grant, protocol, and credential checks. Invalid identities, missing membership, disabled `cerebro_embedded_chat`, and non-API sessions fail closed.
