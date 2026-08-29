# Team Delta Operational Controls Reconciliation

Date: 2026-08-29

Status: design and audit complete, implementation not started

Scope: action audit logging, agent tool policy, approval gates, operator approval queue, operations dashboard, migration safety, API compatibility

## Executive decision

Build the operational controls again on current `fork/main` behind explicit capability gates. Reuse current mainline permission, MCP discovery, schema-digest approval, dashboard, React Query, WebSocket, and secret-audit patterns. Reuse only the requirements and bounded validation ideas from the legacy Phase 3 work. Do not cherry-pick the legacy implementation.

The legacy Phase 3 approval work is not shippable. Its migrations collide with current mainline, violate current migration rules, introduce forbidden foreign keys and cascading actions, retain argument summaries that can disclose secrets, lack complete request handlers and daemon gating, and are based on an obsolete runtime architecture.

## Audit basis and provenance

| Item | Audited value |
|---|---|
| Current Team Delta checkout | `CCRBrad/multica-delta-operational-controls` |
| Current checkout HEAD | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` |
| Local `fork/main` | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` |
| Live `origin/main` | `64ec7f54163d918d5d7fd4dcae857f241b7842d0` |
| Fork delta from live upstream | 2 commits ahead, 0 behind |
| Dirty Phase 3 inspection path | `/Users/bradstrawbridge/.config/superpowers/worktrees/multica/phase3` |
| Phase 3 branch and HEAD | `feat/phase3-operational-controls` at `284facd686121ce7feeaff7b7ad171a208232eae` |
| Phase 3 merge base with current fork | `a06fc273406d75ff67b07a052333c99870be2d39` |
| Phase 3 divergence from current fork | 1,188 current-fork commits versus 18 legacy commits |
| Phase 3 tracking state | Remote tracking branch is gone |
| Phase 3 worktree state at audit | Clean, with no staged, unstaged, or untracked files |
| Archived dirty snapshot | `refs/stash` at `0f5927be70ff1ad3c639c7efa6a81517436a15b4` |
| Archived untracked-file tree | `fddb765b2731d7c85ee3c30dec847b077c9b33ee` |
| Highest migration prefix on current fork | `441` |

The requested dirty worktree is clean now. The only recoverable dirty Phase 3 approval artifacts are in the archived stash named above. This report distinguishes committed branch artifacts from that archived dirty snapshot and does not treat either as current mainline functionality.

This was a read-only ref and source audit. Historical Phase 3 code was not applied and its tests were not executed against current mainline.

## 1. Functionality already shipped on `fork/main`

### 1.1 Shipped foundations

| Capability | Current implementation | What can be reused | Current limitation |
|---|---|---|---|
| Task event persistence | `task_message` persistence in `server/internal/handler/daemon.go`, protocol event `task:message`, and shared transcript views under `packages/views/common/task-transcript/` | Existing task identity, message sequencing, redaction path, and React Query fanout | A task transcript is not an append-only tool audit ledger and does not prove every tool call was intercepted |
| Agent activity | Agent Activity tab in `packages/views/agents/components/tabs/activity-tab.tsx` | Current agent-detail composition and shared view conventions | No dedicated tool action history exists |
| Secret reveal audit | `server/internal/handler/agent_env.go` writes `agent_env_revealed` to `activity_log`; reveal fails closed if the audit write fails; env updates and audit use one transaction | This is the strongest model for approval decisions and sensitive reads | It covers agent environment management, not tool execution |
| Agent MCP configuration | `mcp_config` is redacted for callers that lack access | Existing authorization and response-redaction patterns | Configuration alone is not an invocation allowlist or approval gate |
| Workspace MCP library and agent assignment | Migrations `315` through `318`, `server/internal/handler/workspace_mcp_api.go`, workspace core queries/mutations, and current settings and agent MCP views support explicit server assignment | Secret-free response model, role checks, server-authoritative mutations, and separate workspace-server versus agent-binding resources | Assignment controls which servers an agent receives. Its custom writer guards are not a substitute for `RequireHumanActor` because cloud credentials are not uniformly rejected. |
| Agent integration allowlist | `composio_toolkit_allowlist` is persisted, owner-visible, and edited through current core/view seams | Wholesale replacement semantics, redaction, and agent-owner authorization tests | It controls toolkit overlays, not a provider-neutral per-tool policy |
| Agent invocation permissions | Migration `130_agent_invocation_permission`, `agent.permission_mode`, `agent_invocation_target`, access handlers, and current access settings control who may invoke or view an agent | Existing owner, target, visibility, and compatibility rules | This is trigger access, not an action-level approval gate. |
| Plugin MCP approval | Migration `369_plugin_mcp_approvals`, `server/internal/handler/plugin_mcp.go`, core plugin queries, and `packages/views/plugins/plugin-mcp-approval.tsx` | Discover first, approve no tools by default, pin approved tool schema digests, and block silent schema drift | This is configuration-time approval, not per-invocation approval. Its current admin-only `PUT` does not use `RequireHumanActor`, so reuse the schema-pin concept, not the route guard unchanged. |
| Managed remote MCP broker | `server/internal/service/plugin_mcp_transport.go` and `server/internal/daemon/remote_mcp_broker.go` refuse an unapproved connection, validate pinned schemas, filter discovery, resolve credentials just in time, and reject a disallowed call before forwarding | This is the strongest current Multica-owned pre-transport enforcement point and should be extended | It covers the current plugin remote MCP path, not every native or local tool transport. |
| Privacy-bounded plugin telemetry | Migration `362_plugin_hook_engine` stores status, latency, and bounded host-generated errors without request or response bodies | Use this as the privacy baseline for action events | It is transient plugin execution triage, not a durable general audit ledger. |
| Human-only route guard | `server/internal/handler/actor_guards.go` provides `RequireHumanActor` and tests machine credential rejection | Apply it to every operator decision route | It must still be combined with workspace and role authorization |
| Usage and error dashboard | Shared web/desktop dashboard in `packages/views/dashboard/components/dashboard-page.tsx` with six additive endpoints in `server/internal/handler/dashboard.go` | Date-window parsing, project filtering, restricted-agent filtering, query structure, loading/error/empty states, and shared routing | No operations or approval metrics exist |
| API response compatibility | `packages/core/api/schema.ts` and client methods use Zod plus `parseWithFallback` | Reuse schema validation and malformed-response testing | Legacy Phase 3 bypasses validation, while current fallback logging includes rejected payloads and must be sanitized for this secret boundary. |
| Realtime server state | Protocol events plus `packages/core/realtime/use-realtime-sync.ts` update or invalidate React Query | Add a small approval-change invalidation event | No approval event exists; server state must not be copied into Zustand |
| Runtime capability negotiation | Daemon requests advertise client capabilities; server capabilities are returned by heartbeat acknowledgments | Introduce transport-scoped policy and approval capabilities through the heartbeat state | Version strings and global capability bits are insufficient for mixed transports |
| Public API audit contract | `server/pkg/publicapi/v1` distinguishes planned and enforced audit lifecycle states | Preserve the explicit distinction between planned and enforced | Most of this is a contract surface, not an operational audit sink |

### 1.2 What is not shipped

Current `fork/main` has no general agent tool policy, no append-only MCP or tool action ledger, no per-invocation approval request state machine, no operator approval queue, no approval WebSocket event, and no operations dashboard backed by approval or action data.

The plugin MCP approval feature must not be described as a substitute. It approves a discovered tool schema for configuration use. It does not pause a concrete invocation, ask a human, atomically consume a decision, or prove that the invocation never reached the upstream tool.

## 2. Legacy Phase 3 artifacts

### 2.1 Relevant committed history

These are the committed operational-control changes unique to the legacy branch:

| Commit | Legacy intent | Decision |
|---|---|---|
| `079c99c6c33f1b9b9eb1b26d02c9f4285c6ef9b5` | Add operational and hybrid agent modes, prompt behavior, and UI | Discard |
| `2a7b5f66313be35852b58c252c2654244f2c183d` | Add `agent_action_log` table | Rewrite |
| `84cc1b7ea0a42f42d65c4665a152d102491a8963` | Add action-log queries and generated sqlc output | Rewrite queries; regenerate output |
| `9ecba3dbb9d12ec1c430f6c184186cd919b26d54` | Log task completion and failure as best-effort actions | Discard |
| `0868fa1a579bc5dc24751b963827d83969abe217` | Add `allowed_tools` storage | Reuse semantics selectively; rewrite storage |
| `7c93031e63d2fa230344751eff7335e86c42cd85` | Expose `allowed_tools` through the broad agent API | Rewrite as a dedicated policy API |
| `da4bd0380e7cf5d69e3cb64ac46ad82a72ccbab3` | Add old daemon enforcement, action API, and agent-detail tabs | Reuse tests and bounded validation ideas only; rewrite implementation |
| `284facd686121ce7feeaff7b7ad171a208232eae` | Specify approval queue and operations dashboard | Reuse product requirements and state-machine intent; replace the technical design |

Ten provider-specific intermediate commits are outside the approved scope. They must not be copied, documented as dependencies, or restored.

### 2.2 Exact committed operational file inventory

The seven relevant implementation commits through `da4bd0380e7cf5d69e3cb64ac46ad82a72ccbab3` change exactly 48 files, with 1,472 insertions and 39 deletions. The following paths are those operationally relevant committed files. A path may have been touched by more than one commit.

The in-scope committed frontend surface is exactly three core files, six agent-view files, four locale files, and one design document. It adds no web page, desktop route, navigation item, path definition, or in-scope frontend test. The archived dirty snapshot adds no `packages/` or `apps/` files.

Agent mode and shared client files:

- `packages/core/api/client.ts`
- `packages/core/types/agent.ts`
- `packages/core/types/index.ts`
- `packages/views/agents/components/agent-detail-inspector.tsx`
- `packages/views/agents/components/agent-overview-pane.tsx`
- `packages/views/agents/components/create-agent-dialog.tsx`
- `packages/views/agents/components/inspector/mode-picker.tsx`
- `packages/views/agents/components/tabs/actions-tab.tsx`
- `packages/views/agents/components/tabs/allowed-tools-tab.tsx`
- `packages/views/locales/en/agents.json`
- `packages/views/locales/ja/agents.json`
- `packages/views/locales/ko/agents.json`
- `packages/views/locales/zh-Hans/agents.json`

Server, daemon, and runtime files:

- `server/cmd/server/router.go`
- `server/internal/daemon/agent_tools.go`
- `server/internal/daemon/agent_tools_test.go`
- `server/internal/daemon/daemon.go`
- `server/internal/daemon/execenv/execenv.go`
- `server/internal/daemon/execenv/runtime_config.go`
- `server/internal/daemon/execenv/runtime_config_sections.go`
- `server/internal/daemon/prompt.go`
- `server/internal/daemon/types.go`
- `server/internal/handler/agent.go`
- `server/internal/handler/agent_action_log.go`
- `server/internal/handler/agent_action_log_test.go`
- `server/internal/handler/agent_allowed_tools.go`
- `server/internal/handler/agent_allowed_tools_test.go`
- `server/internal/handler/daemon.go`
- `server/internal/service/builtin_skills/multica-operational-workflow/SKILL.md`
- `server/pkg/agent/agent.go`
- `server/pkg/agent/agent_test.go`
- `server/pkg/agent/claude.go`
- `server/pkg/agent/claude_test.go`
- `server/pkg/agent/codebuddy.go`
- `server/pkg/agent/codebuddy_test.go`

Database and migration files:

- `server/migrations/134_agent_mode.down.sql`
- `server/migrations/134_agent_mode.up.sql`
- `server/migrations/135_agent_action_log.down.sql`
- `server/migrations/135_agent_action_log.up.sql`
- `server/migrations/136_agent_allowed_tools.down.sql`
- `server/migrations/136_agent_allowed_tools.up.sql`
- `server/migrations/137_agent_action_log_task.down.sql`
- `server/migrations/137_agent_action_log_task.up.sql`
- `server/pkg/db/generated/agent.sql.go`
- `server/pkg/db/generated/agent_action_log.sql.go`
- `server/pkg/db/generated/models.go`
- `server/pkg/db/queries/agent.sql`
- `server/pkg/db/queries/agent_action_log.sql`

Design artifact:

- `docs/superpowers/specs/2026-08-28-agent-approval-queue-and-operations-dashboard-design.md`

### 2.3 Exact archived dirty approval inventory

The current Phase 3 worktree is clean. The following incomplete approval changes exist only in the archived dirty snapshot.

The archived tracked worktree delta contains 16 files: six provider-independent approval files listed below and ten excluded provider-track files. Its index delta is empty. The archived untracked tree adds exactly four provider-independent approval files.

Tracked modifications in `refs/stash`:

- `server/internal/handler/agent.go`
- `server/internal/handler/agent_allowed_tools.go`
- `server/internal/handler/agent_allowed_tools_test.go`
- `server/pkg/db/generated/agent.sql.go`
- `server/pkg/db/generated/models.go`
- `server/pkg/db/queries/agent.sql`

Untracked files in archived tree `fddb765b2731d7c85ee3c30dec847b077c9b33ee`:

- `server/migrations/138_agent_approval_queue.down.sql`
- `server/migrations/138_agent_approval_queue.up.sql`
- `server/pkg/db/generated/agent_approval_request.sql.go`
- `server/pkg/db/queries/agent_approval_request.sql`

There are no approval route registrations, complete request handlers, daemon wait or consume logic, WebSocket events, React Query hooks, operator queue components, operations dashboard components, or end-to-end tests in the archived approval snapshot.

### 2.4 Incomplete or unsafe details

| Finding | Why it is unsafe or incomplete | Required response |
|---|---|---|
| Migration prefixes `134` through `138` | Current mainline already owns those numeric prefixes. Current files at `134` through `138` include runtime-profile, comment-index, and search-index work. The migration linter rejects new duplicate numeric prefixes. | Never rename and replay blindly. Rebuild after rebasing and allocate new prefixes from the then-current head. |
| Legacy table migrations | Indexes are created non-concurrently and several statements share one migration. | Use one statement per concurrent index migration and the current migration rules. |
| Archived approval migration | It adds foreign keys with `ON DELETE CASCADE` and `ON DELETE SET NULL`. Both violate repository policy. | Use direct workspace identifiers and explicit application cleanup in transactions. |
| Implicit indexes | Inline primary key and unique constraints create indexes outside the required concurrent-index workflow. | Build unique indexes concurrently, then attach constraints in separate single-statement migrations. |
| `agent_action_log` identity and scope | Agent, issue, and task identifiers are loose text; rows lack direct `workspace_id`; recent-action queries can be global. | Use UUID columns, direct workspace scoping, cursor pagination, and workspace predicates in every query. |
| Raw summaries | `args_summary` and `result_summary` allow up to 4,096 bytes and are returned to the UI. Regex redaction is not proof that secrets cannot persist. | Persist no argument or result values. Store approved schema identity, shape metadata, sizes, outcome codes, and links only. |
| Policy disclosure | Legacy `allowed_tools` values are included in general Agent responses with no redacted marker. Arbitrary strings can expose internal identifiers or misplaced credentials to viewers and subscribers. | Keep policy behind its dedicated permissioned endpoint and return redacted capability summaries on broad Agent resources. |
| Best-effort audit writes | Failures only produce warnings while task execution continues. | Pre-execution policy and approval records must commit before transport. Operator decisions and their audit events must commit atomically. |
| Tool lifecycle model | Tool use and tool result can become separate rows tied only by task and message sequence. Task completion is also treated as a tool action. | Use one invocation ID and append immutable state events. Keep task lifecycle in the existing task model. |
| Action list lookup | The legacy handler resolves an agent, then queries with the raw URL value rather than the resolved UUID. A human-readable agent ID can authorize correctly but return an incorrect empty result. | Query only with the resolved agent UUID and direct workspace ID. |
| UTF-8 truncation | Summary truncation slices raw bytes at 4,096 and can split a multibyte code point, causing a PostgreSQL text error. | Store no raw summary. Apply the current PostgreSQL text sanitizer to every remaining bounded text field. |
| Allowlist enforcement | Enforcement is an old provider switch plus CLI flags. It does not establish universal interception, and several current runtimes did not exist on the legacy branch. | Advertise exact capabilities. Fail closed when a configured policy cannot be enforced. |
| Task-actor self-escalation | The legacy broad `UpdateAgent` route uses owner-derived `canManageAgent` behavior without `RequireHumanActor`. A task credential can act through its owner identity, change `allowed_tools`, and read action summaries. | Never expose policy mutation or operator audit reads through the broad agent route. Apply explicit human-only guards and separate task-scoped daemon endpoints. |
| Wildcard matching | Archived approval parsing accepts a global wildcard and trailing prefix wildcards. That makes schema drift and broad privilege hard to reason about. | Start with exact canonical tool identities plus schema digests. Add wildcard policy only through a separately reviewed design. |
| Broad agent API | Policy fields are mixed into create and update agent payloads. Null and empty values have different meanings that old clients can accidentally change. | Use a dedicated versioned policy endpoint with wholesale replacement and optimistic concurrency. |
| Approval insert query | `ON CONFLICT DO NOTHING RETURNING *` returns no row on a conflict, so it does not implement the claimed idempotent create-or-get behavior. | Use an insert-or-select transaction or CTE and verify immutable identity on retries. |
| Approval state consistency | The archived table has no timestamp-state consistency rules, no cancellation query, no retention query, and no bounded maximum expiry interval. Consume validates only request and task IDs, not the stable tool-call identity. | Enforce guarded transitions, stable invocation identity, bounded expiry, cancellation, retention, and timestamp invariants. |
| Approval authorization | The archived query layer joins through agent for workspace scope and has no decision handlers or human-only route guard. | Put `workspace_id` on every operational row and enforce `RequireHumanActor` plus owner/admin role checks. |
| Approval consumption | SQL exists, but no daemon blocks before transport, waits for a decision, consumes it exactly once, or handles disconnect and cancellation. | Implement and test a server-owned state machine and a managed transport gate. |
| Legacy actions tab | It uses ad hoc query keys without workspace identity, raw API casting, hard-coded strings, direct API access from `packages/views`, no polling or realtime invalidation, and raw summary rendering. | Rewrite through workspace-scoped core query options, Zod parsing, localization, realtime invalidation, and metadata-only views. |
| Legacy allowed-tools tab | It is a raw textarea with no permission input, discovery, schema digest, capability status, or structured selection. Blank input sends unrestricted `null`, deny-all cannot be expressed, local state does not reset when the agent changes, and clearing has no warning. | Rewrite using discovered tools, explicit support status, exact rules, authoritative permissions, deny-all support, agent-keyed state, and confirmation for privilege expansion. |
| Legacy mode creation and update | Mode can be created or changed without atomically establishing a tool restriction, so a nominally operational agent can remain unrestricted. Current mainline also has multiple creation and resume flows that the old dialog does not cover. | Discard the mode feature. Apply policy through the dedicated current workbench flow after all current creation paths are modeled. |
| Legacy agent-detail patches | Current mainline has a materially different overview, inspector, creation draft, duplicate, AI builder, resume, web, and desktop architecture. | Do not apply the old patches. Integrate new controls into current shared surfaces. |
| Generated sqlc files | Generated files reflect the old schema and old mainline. | Discard and regenerate from reconciled queries. |
| Legacy operations design | It assumes the unsafe schema and old route/component structure. | Preserve product outcomes only and use current shared dashboard architecture. |

## 3. Reconciled schema and API plan

### 3.1 Design invariants

1. No foreign keys and no cascading database actions.
2. Every operational row carries `workspace_id` directly.
3. No tool argument values, result values, environment values, command lines, headers, URLs, or free-form error bodies enter new operational-control tables, API responses, WebSocket payloads, or logs.
4. Exact canonical tool identity and approved schema digest are the first release policy keys. No wildcard rules in the first release.
5. Absence of a policy preserves legacy behavior. Once a policy exists, its default is deny.
6. An approval-required rule is also an allowed rule. Approval cannot bypass the allowlist.
7. A `require_approval` tool may execute only after its pending request becomes approved and that approval is atomically consumed exactly once. An `allow` rule does not create an approval request.
8. Capabilities are scoped by transport and provider family. A boundary that cannot intercept before invocation advertises no approval capability, and a rule cannot activate against that boundary.
9. Tool audit coverage is reported honestly by transport. Unobserved native CLI behavior is never presented as complete audit coverage.
10. Operator decisions are human-only, workspace-scoped, and transactionally audited.
11. Policy replacement cancels every unconsumed request from an older revision. Consume revalidates the active revision and exact current rule in the same transaction.
12. Every policy change has an immutable, server-generated revision snapshot and policy digest.
13. A policy-protected agent is not claimable until its initial policy is active. Opt-in and activation are fenced server-side so creation cannot race invocation.
14. Release one enforcement is limited to the current managed remote MCP broker. Other transports remain explicitly unsupported until they gain their own pre-forward boundary and scoped capability.

### 3.2 Tables

#### `agent_tool_policy`

One current policy header per agent:

- `id UUID NOT NULL DEFAULT gen_random_uuid()`
- `workspace_id UUID NOT NULL`
- `agent_id UUID NOT NULL`
- `revision BIGINT NOT NULL DEFAULT 1`
- `status TEXT NOT NULL DEFAULT 'draft'`, either `draft` or `active`
- `policy_digest TEXT NOT NULL`
- `default_effect TEXT NOT NULL DEFAULT 'deny'`
- `created_by_user_id UUID NOT NULL`
- `updated_by_user_id UUID NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Checks bound `default_effect` to `deny`, constrain status, validate digest shape, and require positive revision. A unique concurrent index on `agent_id` provides one policy per agent. A separate concurrent index supports `(workspace_id, agent_id)` lookup.

#### `agent_tool_policy_revision`

An immutable server-generated snapshot stores `workspace_id`, `agent_id`, `revision`, `policy_digest`, actor user ID, canonical metadata-only rule identities, and `created_at`. The server canonicalizes the snapshot; callers never submit stored JSON directly. A unique concurrent index covers `(agent_id, revision)`. Policy replacement writes the revision snapshot, updates the current header and rules, cancels older unconsumed approvals, and records the policy audit entry in one transaction.

#### `agent_tool_policy_rule`

One exact tool rule per approved schema:

- `id UUID NOT NULL DEFAULT gen_random_uuid()`
- `workspace_id UUID NOT NULL`
- `agent_id UUID NOT NULL`
- `policy_id UUID NOT NULL`
- `transport_kind TEXT NOT NULL`
- `server_key TEXT NOT NULL`
- `tool_name TEXT NOT NULL`
- `schema_digest TEXT NOT NULL`
- `effect TEXT NOT NULL`, either `allow` or `require_approval`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`

All identity strings are length-bounded and normalized. The digest uses the already established `sha256:<hex>` shape. A unique concurrent index covers `(agent_id, transport_kind, server_key, tool_name)`, so only one active digest can exist for a canonical tool. There is no foreign key to the header.

#### `agent_tool_approval_request`

The mutable current state for one concrete invocation:

- `id UUID NOT NULL DEFAULT gen_random_uuid()`
- `workspace_id UUID NOT NULL`
- `agent_id UUID NOT NULL`
- `task_id UUID NOT NULL`
- `issue_id UUID`
- `chat_session_id UUID`
- `invocation_id UUID NOT NULL`
- `idempotency_key TEXT NOT NULL`
- exact `transport_kind`, `server_key`, `tool_name`, and `schema_digest`
- `policy_revision BIGINT NOT NULL`
- `schema_field_names TEXT[] NOT NULL DEFAULT '{}'::text[]`
- `argument_bytes INTEGER NOT NULL DEFAULT 0`
- `status TEXT NOT NULL DEFAULT 'pending'`
- `reason_code TEXT`
- `requested_at`, `decided_at`, `consumed_at`, and `expires_at`
- `decided_by_user_id UUID`

`schema_field_names` is derived by the server from the pinned schema, never from daemon-supplied argument data. It may contain no values or dynamic keys. The first release uses enumerated reason codes rather than a free-form decision note.

The state machine is:

```text
pending -> approved -> consumed
pending -> denied
pending -> expired
pending -> cancelled
approved -> expired
approved -> cancelled
```

Unique concurrent indexes on `(task_id, idempotency_key)` and `(task_id, invocation_id)` bind one request to one invocation. Pending queue indexes include `workspace_id` first. Every create retry and transition verifies workspace, task, agent, invocation, canonical tool identity, schema digest, policy revision, expected status, and expiry. Consume also locks and verifies the active policy revision and current exact rule.

#### `agent_tool_action_event`

An append-only metadata ledger:

- `id UUID NOT NULL DEFAULT gen_random_uuid()`
- `workspace_id UUID NOT NULL`
- `agent_id UUID NOT NULL`
- `task_id UUID NOT NULL`
- `issue_id UUID`
- `invocation_id UUID NOT NULL`
- `approval_request_id UUID`
- exact tool identity and `schema_digest`
- `transport_kind TEXT NOT NULL`
- `coverage_kind TEXT NOT NULL`
- `event_type TEXT NOT NULL`
- `argument_bytes`, `result_bytes`, and `duration_ms`
- bounded `outcome_code` and `error_class`
- `actor_user_id UUID` for human decision events only
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Allowed events are `requested`, `policy_allowed`, `policy_denied`, `approval_requested`, `approval_approved`, `approval_denied`, `approval_expired`, `approval_consumed`, `started`, `succeeded`, `failed`, and `cancelled`. A unique concurrent index on `(workspace_id, task_id, invocation_id, event_type)` makes retries task- and tenant-scoped. Server transitions alone emit policy and approval events. The daemon may report only `started`, `succeeded`, `failed`, and `cancelled`, with no actor fields. If repeated event types are later required, add an explicit event sequence.

`coverage_kind` distinguishes `managed_mcp`, `managed_native`, and `declaration_only`. Dashboard totals must expose coverage and must never combine declaration-only counts with proven intercepted invocations without a label.

Database checks explicitly constrain policy status, rule effect, approval status, event type, coverage kind, reason code, digest form, timestamp-state consistency, nonnegative sizes and duration, and maximum approval lifetime. Host-generated `outcome_code` and `error_class` are bounded enums, not copied error text.

### 3.3 Migration allocation

At this audit snapshot the highest prefix is `441`, so `442` is the current next candidate. Do not reserve or write that number until the implementation branch rebases onto the live target. The schema owner must then:

1. Re-run the migration prefix audit.
2. Allocate one contiguous, documented block from the actual next free prefix.
3. Put table creation or column checks in single-purpose migrations with no inline index-producing primary or unique constraints.
4. Create every primary, unique, and secondary index with `CREATE UNIQUE INDEX CONCURRENTLY` or `CREATE INDEX CONCURRENTLY`, one statement per migration file.
5. Attach primary or unique constraints to completed unique indexes with separate single-statement `ALTER TABLE ... USING INDEX` migrations.
6. Add matching down migrations that do not pretend a concurrent index can be dropped inside an incompatible transaction mode.
7. Run the migration linter, fresh-database migration tests, and upgrade-from-current tests before sqlc generation.

Generated sqlc output is produced only after migrations and handwritten queries settle. None of the legacy generated files should be merged.

The legacy audit work added exactly nine relevant test functions across six test files. The archived approval work added one parser test function. These cover parser bounds, two provider argument builders, and limited summary redaction, but they do not cover migrations, SQL integration, cross-workspace access, task-actor rejection, stable invocation correlation, approval transitions, realtime, or end-to-end gate-before-forward behavior.

### 3.4 Cleanup and retention

Application services own relational validation and cleanup:

- Agent deletion removes active policy rows and rules and cancels pending approvals in one transaction.
- Historical terminal approval records and action events remain for the configured audit-retention period, identified by immutable agent ID.
- Workspace deletion explicitly removes all operational-control rows inside the existing workspace deletion transaction.
- Task cancellation atomically cancels pending or approved, unconsumed requests.
- Expiry is enforced both by guarded state transitions and an idempotent sweeper.

The release owner must set and document the retention period before production enablement. Retention deletion queries must be workspace-bounded and covered by integration tests.

### 3.5 API surface

All endpoints are additive and return metadata only. Public app config exposes `operational_controls_v1`, defaulting to false, so old servers are detected before feature calls. Sensitive schemas return a discriminated `available`, `unsupported`, or `unreadable` state and use sanitized validation logging that records endpoint and issues, never the rejected payload.

Agent policy and history:

- `GET /api/agents/{agentId}/tool-policy`
- `PUT /api/agents/{agentId}/tool-policy`
- `GET /api/agents/{agentId}/tool-actions?cursor=&limit=&event_type=&since=`

The policy `PUT` is a wholesale replacement with `expected_revision`. It uses `RequireHumanActor`, owner/admin role checks, and a machine-credential handler backstop. A stale revision returns `409`. Replacement and first activation are transactional, cancel older unconsumed approvals, and verify the selected transport and provider-family capabilities. A protected agent remains unclaimable while its policy is draft.

Operator approval queue:

- `GET /api/approvals?status=pending&agent_id=&cursor=&limit=`
- `GET /api/approvals/{approvalId}`
- `POST /api/approvals/{approvalId}/decision`

The decision body contains `decision`, `reason_code`, and `expected_status`. The handler uses `RequireHumanActor`, verifies workspace owner or admin, locks the request, applies the guarded transition, appends the action event, and writes an `activity_log` decision record in one transaction. Any audit failure rolls the decision back.

Daemon control plane:

- `POST /api/daemon/tasks/{taskId}/tool-invocations`
- `GET /api/daemon/tasks/{taskId}/tool-approvals/{approvalId}`
- `POST /api/daemon/tasks/{taskId}/tool-approvals/{approvalId}/consume`
- `POST /api/daemon/tasks/{taskId}/tool-invocations/{invocationId}/events`

Create is idempotent by task and idempotency key and returns a server-created invocation ID. A retry returns the existing invocation only after comparing workspace, agent, tool, schema, and policy revision fields. Consume is atomic, matches that server-created invocation, revalidates the active revision and current exact rule, and returns authorization exactly once. Daemon credentials are task-bound and can request, observe, consume, or append only transport lifecycle events. They can never emit policy or approval events, set actor fields, approve, or deny.

Dashboard:

- `GET /api/dashboard/operations/summary?days=&tz=&project_id=`
- `GET /api/dashboard/operations/by-agent?days=&tz=&project_id=`
- `GET /api/dashboard/operations/recent?cursor=&limit=&project_id=`

These owner/admin-only endpoints follow current dashboard date parsing and restricted-agent filtering. Summary fields include pending, approved, denied, expired, failed, intercepted invocation count, declaration-only count, median decision time, and configured-agent capability gaps. A malformed response becomes `unreadable`, never an empty queue, absent policy, or zeroed success state.

Pending is a current count and ignores the historical `days` window. Decision and action aggregates honor the window. Project filtering applies only to task-linked metrics and includes an explicit unlinked bucket; capability-gap counts are agent-level and ignore project. The UI labels each scope.

### 3.6 Mixed-version behavior

- New server plus old daemon: no applicable transport-scoped capability appears in heartbeat state. Agents without a policy retain current behavior. An agent with a configured policy is rejected before claim or spawn with an actionable unsupported-runtime error.
- Old server plus new daemon: absent heartbeat capability means the daemon does not use new endpoints and does not claim enforcement.
- New client plus old server: public config defaults the feature off. If a mixed deployment still returns `404`, the operational adapter catches only that feature-absence case and returns `unsupported`; authorization and server errors remain visible. Zod fallback is not used for non-2xx responses.
- Old client plus new server: additive endpoints and events are ignored. Existing agent payloads do not gain policy fields, so old updates cannot clear policy.
- Schema drift: a digest change has no matching exact rule and is denied until a human replaces the policy.

## 4. Work packages

### 4.1 Backend and database

- Own the migration prefix allocation and all five table families.
- Add handwritten sqlc queries with direct `workspace_id` predicates and cursor pagination.
- Implement exact policy matcher and schema-digest validation in a provider-neutral package.
- Add policy read and wholesale-replace handlers.
- Add a provider-neutral discovery contract only when a transport has a managed enforcement boundary. Release one adapts the existing broker discovery and schema-digest contract.
- Add append-only event writer and metadata-only action-list handler.
- Implement approval create-or-get, lock, decide, consume, expire, cancel, and sweep transitions.
- Make request creation plus `approval_requested`, consume plus `approval_consumed`, expire or cancel plus its event, and operator decision plus `activity_log` and its event atomic transactions.
- Extend workspace and agent cleanup services explicitly, with no database cascades.
- Add dashboard aggregates that honor existing restricted-agent filters and date windows.
- Add bounded structured logs containing identifiers and codes only.

### 4.2 Daemon and managed tool transport

- Advertise policy and approval capabilities per transport and provider family only when enforcement occurs before forwarding.
- Ask the server to bind each observed call to a UUID invocation ID and exact canonical tool identity before forwarding.
- Add an optional invocation ID to the tool-use and tool-result wire and persistence path so transcript entries and action events correlate. Old senders may omit it; capability-aware senders must provide it.
- Enforce in this order: capability, exact allowlist, exact schema digest, approval requirement, atomic consume, transport invocation.
- For approval-required calls, create or fetch the request and wait through bounded polling or server notification. Cancellation and expiry stop the wait.
- Never send argument or result values to operational endpoints or logs.
- If the pre-execution audit or approval write fails, do not call the tool.
- Persist tool-result task messages and their terminal action events in one server transaction. Pre-forward `started` must commit before the broker call. Do not rely on an in-memory daemon retry for audit-grade events.
- Keep native provider flags as defense in depth where supported. Do not represent them as universal interception.
- Extend the current managed remote MCP broker first, because it already rejects before upstream forwarding and keeps credentials out of the claim wire type.

### 4.3 Frontend and core packages

- Add sanitized schemas, discriminated unavailable states, API methods, workspace-scoped query keys, query options, and non-optimistic mutations in `packages/core`.
- Add pure permissions for reading policy and actions, replacing policy, reading and deciding approvals, and viewing Operations. Do not reuse broad `canEditAgent`.
- Keep all server state in React Query. Use no new Zustand server-state store.
- Replace the legacy textarea with structured exact tool rows from discovery, including schema digest and runtime capability state.
- Integrate policy controls into the current agent access or MCP surface rather than restoring the old agent-detail structure.
- Across every current creation and resume flow, a user selecting protected operation creates the agent disabled, then activates it only with the initial policy transaction.
- Build a shared `packages/views` approval queue at centralized path `/approvals`, with a sidebar entry, thin web page, desktop route, pending-first ordering, filters, safe issue/chat/agent fallback destinations, accessible expiry display, and confirmed approve or deny controls.
- Add `?tab=operations` to the existing shared `/usage` dashboard union, parser, keys, freshness, refresh, and unknown-tab fallback. Hide and disable its queries until owner/admin membership is resolved.
- Show metadata only: tool identity, schema digest, server-derived schema field names, task context link, timestamps, reason code, outcome, and coverage kind.
- Localize all visible strings and cover loading, empty, permission-denied, expired, stale-decision, unsupported-runtime, and error states.
- Approval decisions are non-optimistic: disable repeat submission, await the authoritative response, retain the row on failure, reconcile `409` or expiry, invalidate after settle, and restore focus. Countdown text uses server status as authority and announces only state transitions, not every tick.

### 4.4 WebSocket and realtime

- Add a role-targeted `operational_controls:changed` invalidation event with only `workspace_id`. Send it only to authorized human owner/admin connections after policy, action, request, decision, consume, expiry, or cancellation commits.
- The client validates active workspace, coalesces events, and invalidates approval, policy, action-history, and Operations React Query keys. Add the same keys to reconnect invalidation.
- Human agent owners who can read their own action metadata use bounded polling because they do not receive the workspace operator event.
- Do not put tool names, arguments, results, decision notes, or secret-adjacent data in the event.
- Do not broadcast protected resource IDs or every action globally. Rate-limit the role-targeted invalidation event.
- On role downgrade, ownership transfer, or unauthorized response, remove protected policy, action, queue, and Operations caches rather than only disabling queries.
- Confirm old clients ignore the unknown event and reconnect without inheriting stale capability state.

### 4.5 Permission model

| Action | Human workspace owner/admin | Human agent owner | Workspace member | Agent or task credential | Daemon task credential |
|---|---:|---:|---:|---:|---:|
| Read effective policy | Yes | Yes for owned agent | No | No | Task-scoped subset only |
| Replace policy | Yes | No in first release | No | No | No |
| Read agent action metadata | Yes | Yes for owned agent | No | No | Task-scoped only |
| Read workspace approval queue | Yes | No | No | No | No |
| Approve or deny | Yes, with `RequireHumanActor` | No | No | No | No |
| Create or observe a task approval | No through operator API | No | No | No | Yes for bound task |
| Consume an approval | No through operator API | No | No | No | Yes for bound task, once |
| View operations dashboard | Yes | No | No | No | No |

Server authorization is authoritative. Hiding a frontend control is not an access control. Cross-workspace IDs must return the repository's standard not-found or forbidden behavior without revealing existence.

### 4.6 Test package

Database and migrations:

- Duplicate-prefix lint and current-next-prefix checks.
- Fresh install, upgrade from current fork, and down/up cycles.
- Static checks for no foreign keys, no cascades, and every index created concurrently in a single-statement migration.
- Constraint attachment, idempotency, direct workspace predicates, cursor ordering, and explicit cleanup tests.

Backend:

- Owner/admin, agent owner, member, agent actor, task credential, and cross-workspace authorization matrix.
- Human-only decision route tests for every known machine actor source.
- Self-escalation tests that attempt to clear or widen policy through every broad Agent mutation path using task credentials.
- Concurrent approve, deny, expire, cancel, and consume races with exactly one winning transition.
- Policy replacement versus consume races prove stale revisions cannot execute and older unconsumed requests are cancelled.
- Idempotent create returns the existing request and rejects a key reused with different immutable identity.
- Decision, action event, and `activity_log` atomic rollback tests.
- Atomic rollback tests for create, consume, expire, cancel, task-message, and their paired action events.
- Daemon event author tests reject forged policy, approval, and actor fields.
- Secret canaries in arguments, results, headers, URLs, environment, and errors never appear in database rows, API bodies, logs, or WebSocket payloads.
- Dashboard visibility and aggregation tests, including capability-gap and coverage labels.

Daemon:

- Exact allow, deny, schema drift, approval required, unsupported capability, timeout, cancellation, and reconnect cases.
- Assert the mock upstream transport receives zero calls for denied, pending, expired, cancelled, audit-write-failed, and already-consumed cases.
- Assert one upstream call for one consumed approval under concurrent retries.
- Verify no argument or result values enter operational requests or structured logs.
- Test HTTP and WebSocket claim paths advertise and honor the same capabilities.

Core and frontend:

- Malformed-response, sanitized validation logging, explicit old-server `404`, unknown-enum, and unavailable-state tests.
- Query-key invalidation, active-workspace, reconnect, event coalescing, role-downgrade cache eviction, and no Zustand writes.
- Assert unauthorized policy, queue, action, and Operations queries are never issued.
- Queue loading, empty, permissions, stale decision, expiry, and offline states.
- Accessible names, keyboard navigation, focus return, live status announcements, and contrast.
- Shared web and desktop route smoke tests.
- `/approvals` navigation and `?tab=operations` deep-link, authorization, and fallback tests.
- No raw value rendering snapshots.

End to end:

- Use a controlled, non-mutating managed MCP fixture.
- Verify pending means no upstream call.
- Verify approve then consume permits one call.
- Verify deny, expiry, cancellation, duplicate consume, and schema drift permit zero calls.
- Verify the action ledger, task transcript, queue, and dashboard converge without leaking the fixture's secret canary.

## 5. Dependency order and independently implementable slices

```text
S0 contracts and live rebase
 |
 v
S1 schema foundation
 |--------------------------|
 v                          v
S2 policy service       S3 audit service
 |                          |
 |-----------|--------------|
             v
       S4 approval state machine
             |
      |------|--------------------|
      v                           v
S5 daemon gate              S6 core and realtime
      |                           |
      |                    |------|------|
      |                    v             v
      |               S7 queue UI   S8 operations UI
      |                    |             |
      |--------------------|-------------|
                           v
                  S9 end-to-end rollout
```

| Slice | Deliverable | Depends on | Can proceed independently from |
|---|---|---|---|
| S0 | Rebase, final endpoint and enum contract, capability names, migration block allocation | None | None |
| S1 | Tables, concurrent indexes, constraints, sqlc models, cleanup contract | S0 | UI component prototyping with fixed mock contracts |
| S2 | Dedicated policy API, exact matcher, schema-digest drift behavior, permission tests | S1 | Dashboard SQL and approval UI mocks |
| S3 | Append-only action writer, list API, secret-negative tests | S1 | Policy API and frontend queue mocks |
| S4 | Approval create, decide, consume, expire, cancel, sweep, and human-only audit transaction | S2 and S3 | Dashboard presentation work |
| S5 | Daemon capability advertisement and managed pre-invocation gate | S2 and S4 | Frontend implementation |
| S6 | Core schemas, client, React Query, WebSocket invalidation | S2, S3, and S4 contracts | Daemon internals |
| S7 | Shared operator approval queue and agent policy controls | S6 | Dashboard UI and daemon internals |
| S8 | Operations aggregate endpoints and shared dashboard tab | S3 and S4 data; S6 client patterns | Queue UI and most daemon work |
| S9 | Cross-version, race, security, accessibility, web/desktop, and managed MCP end-to-end verification | S5, S7, and S8 | None |

The migration block has one owner in S1. Other slices must not add operational-control migrations independently. This prevents branch-local number allocation from colliding during parallel development.

## 6. Reuse, rewrite, or discard decisions

This disposition applies to every path in the exact inventories above. Shared files such as `agent.go` or `client.ts` follow the decision for the legacy hunk, not for the current file as a whole.

| Legacy artifact or idea | Decision | Reconciled use |
|---|---|---|
| Operational and hybrid agent modes | Discard | They are unrelated to the control boundary and are built on a stale prompt/runtime model. |
| Legacy built-in operational workflow skill | Discard | Tool safety belongs in enforced transport and policy code, not prompt instructions. |
| `agent_action_log` table and migrations | Rewrite | Replace with workspace-scoped, typed, append-only metadata events and compliant migrations. |
| Action log sqlc queries | Rewrite | Add cursor pagination, direct workspace predicates, UUID identity, and metadata-only projections. |
| Generated action-log sqlc files | Discard | Regenerate from new schema and queries. |
| Best-effort task complete/fail logging | Discard | Keep task lifecycle in current task state and make required tool audit writes enforceable. |
| Null means unrestricted; empty allowlist means deny all | Reuse with change | Absence of a policy preserves compatibility. An existing policy defaults to deny and uses exact rules. |
| Entry count, byte bounds, trimming, and duplicate rejection | Reuse | Apply to exact canonical identity fields and API request bounds. |
| Global and prefix wildcard matcher | Discard | Exact identity plus schema digest is the first release policy. |
| `allowed_tools` JSONB column and broad Agent response contract | Discard | Replace with dedicated normalized policy header and rule tables behind a permissioned endpoint. |
| Broad create/update agent policy fields | Discard | Use a dedicated revisioned policy endpoint so old clients cannot clear policy. |
| Old provider switch and CLI-only enforcement | Discard | Use current runtime capabilities and managed pre-invocation transports. Keep supported flags only as defense in depth. |
| Legacy action API handler | Rewrite | Current authorization, direct workspace scope, cursor pagination, Zod client parsing, and no raw summaries. |
| Legacy core Agent type and index exports | Discard as patches | Define dedicated policy, action, approval, capability, and dashboard wire schemas against current core types. |
| Legacy router patch | Discard as a patch | Register the new dedicated routes with current middleware and explicit human or daemon guards. |
| Legacy actions tab | Rewrite | Build through current shared agent-detail and React Query architecture with metadata only. |
| Legacy raw allowed-tools textarea | Discard | Use discovered, schema-pinned structured rows with support status. |
| Legacy overview, inspector, create-dialog, and mode-picker patches | Discard | Integrate policy controls into the current workbench, draft, duplicate, AI builder, resume, web, and desktop flows without restoring mode. |
| Four locale file changes | Reuse only as terminology drafts | Re-key reviewed strings in the current locale structure; discard mode terminology. |
| Legacy provider argument builders and tests | Reuse narrowly | Preserve blocked-custom-argument cases only as defense in depth after the managed broker gate. |
| Legacy daemon execenv, prompt, and type changes | Discard | Add only provider-neutral invocation identity, capabilities, and managed gate behavior to current daemon code. |
| Approval state names and human decision intent | Reuse | Keep pending, approved, consumed, denied, expired, and cancelled with guarded transitions. |
| Archived `approval_required_tools` parser and broad API plumbing | Rewrite | Preserve bounded validation ideas only; use exact dedicated policy rules. |
| Archived approval migration | Discard | It collides, uses forbidden relationships, creates unsafe indexes, and stores raw summaries. |
| Archived approval handwritten queries | Rewrite | Fix create-or-get, direct workspace scope, lock ordering, idempotency identity checks, and cursor pagination. |
| Archived generated approval sqlc file | Discard | It is incomplete and must be regenerated. |
| Legacy approval design document | Reuse requirements; rewrite design | Keep fail-closed gating, human decisions, expiry, queue, and aggregate outcomes. Replace schema, APIs, permissions, routes, and runtime assumptions. |
| Dedicated operator approval queue | Reuse product outcome | Implement as a current shared web/desktop view backed by core query options. |
| Operations dashboard | Reuse product outcome | Add an Operations tab to the existing shared `/usage` dashboard and current endpoint conventions. |
| Missing web page, desktop route, path, navigation, realtime, and frontend tests | Build fresh | Use thin platform adapters, current shared views, current event contracts, and the test package in this report. |
| Provider-specific legacy artifacts | Discard | They are explicitly excluded and have no place in the reconciled dependency graph. |

## 7. Rollout and launch gates

1. Merge S1 through S4 with all features disabled and migrations verified.
2. Ship server and daemon capability negotiation. Confirm mixed-version behavior before any policy can be saved.
3. Enable policy and audit for a controlled managed MCP fixture in a non-production workspace.
4. Prove denied, pending, expired, cancelled, and duplicate-consume paths make zero upstream calls.
5. Prove secret canaries are absent from persistence, API, WebSocket, logs, queue, and dashboard.
6. Enable operator queue for owner/admin users in the controlled workspace.
7. Enable dashboard operations metrics only after coverage labels match observed transport support.
8. Set audit retention and operational alert thresholds before broader enablement.
9. Expand by runtime capability, never by runtime name alone.

No approval feature is shippable until the daemon can demonstrably pause before transport, the server can atomically consume a human decision once, and the end-to-end zero-call assertions pass.

## 8. Implementation paths considered

### Path A: cherry-pick the legacy branch

Rejected. It combines stale runtime assumptions with migration collisions, unsafe schema, incomplete approval handling, and outdated frontend boundaries.

### Path B: reimplement on current mainline seams

Selected. It preserves the proven product intent while using current permission, dashboard, realtime, API parsing, MCP discovery, and migration contracts.

### Path C: introduce a separate policy service

Deferred. It would add another consistency and availability boundary before the in-process managed transport and approval state machine are proven. The proposed tables and APIs leave room to extract a service later without changing operator semantics.

## Final reconciliation conclusion

The current fork contains the right foundations but none of the complete operational-control product. The legacy Phase 3 branch supplies useful requirements, validation limits, and test ideas, yet its implementation must not be shipped or revived. The safe path is an additive, capability-gated rebuild with exact schema identities, metadata-only audit records, human-only atomic decisions, managed pre-invocation enforcement, shared web/desktop surfaces, and explicit mixed-version failure behavior.
