# GitLab Issues Integration — Design Spec

**Date:** 2026-04-17
**Status:** Draft — pending user review
**Author(s):** Jimmy Mills (+ Claude as brainstorming partner)

## Summary

Replace Multica's server-owned `issues` concept with a cache of GitLab issues for one GitLab project per workspace. GitLab becomes the system of record; Multica's server sits in front of it as a cached read-through / write-through proxy. Existing downstream features (inbox, activity log, real-time WS, agent task queue, autopilot) keep working by reading from the cache, which is kept in sync via GitLab webhooks and a reconciliation loop.

Scope is `gitlab.com` only; self-hosted GitLab is out of scope for MVP. The Multica product is not yet live, so no data migration is needed — the new schema is a fresh install.

## Goals

- GitLab is the single source of truth for issue data. A user editing an issue in gitlab.com sees the same result as editing it in Multica.
- Multica's downstream subsystems (inbox, activity, WS, agents, autopilot) continue to function without rewriting each one.
- Agents remain first-class participants. A Multica user can assign an agent to an issue and the agent can comment, change status, and work on it end-to-end.
- Human attribution in GitLab is correct: comments, status changes, and assignments appear under the human who performed them, not a shared bot.

## Non-goals

- Self-hosted GitLab support.
- Multi-project workspaces (a workspace is scoped to exactly one GitLab project).
- OAuth-based GitLab auth (v1 uses PAT paste).
- Sub-issues, issue dependencies, Multica-only fields (`acceptance_criteria`, `context_refs`).
- Data migration from the current Multica issue store (product is pre-launch).
- Key rotation / KMS-backed secret management.

## Architectural decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | **Cached read-through + write-through proxy** (not pure client-side, not thin proxy, not event-sourced) | Keeps all downstream subsystems working; the `issues` table survives as a cache. Near-real-time feel. |
| 2 | **One GitLab project per Multica workspace** | Matches the 1:1 feel of workspaces; simplest config; naturally extends to multi-project later if needed. |
| 3 | **Hybrid auth: per-user PAT for writes, workspace service PAT for reads/fallback** | Preserves human attribution in GitLab while keeping the product usable for users who haven't connected yet. |
| 4 | **PAT paste (not OAuth)** | Ships faster; simple to implement; acceptable v1 UX. OAuth can follow. |
| 5 | **`gitlab.com` only** | Removes base-URL plumbing; narrows scope. |
| 6 | **Status stored as scoped labels (`status::*`) in GitLab** | Keeps Multica's 7-state kanban UX; standard GitLab team convention; honors source-of-truth principle. |
| 7 | **Priority stored as scoped labels (`priority::*`) in GitLab** | Same pattern as status. |
| 8 | **Agent assignees stored as scoped labels (`agent::<slug>`) in GitLab** | No GitLab admin required; visible and editable from GitLab; doesn't consume GitLab user seats. |
| 9 | **Webhook-driven cache with periodic reconciliation** (not poll-only, not event-sourced) | Near-real-time; low GitLab API load; reconciler is a safety net, not the primary path. |
| 10 | **Autopilot keeps working; creates real GitLab issues via service PAT** | Core product feature; simple re-point; autopilot-generated issues are findable in GitLab. |
| 11 | **Fresh install — no migration of existing Multica issue data** | Product is pre-launch. Aligns with CLAUDE.md "no backwards-compat shims when the product is not yet live." |
| 12 | **GitLab labels cached in a normalized table (not `TEXT[]`, not JSONB)** | We rely heavily on scoped labels; label metadata (color, description) needs to be available for UI rendering; filtering stays clean. |

## High-level architecture

```
Read (browser, desktop, agent):
  client → Multica REST /api/issues/... → read cache → respond

Write (browser, desktop, agent):
  client → Multica REST /api/issues/... (PUT)
    → GitLab API (per-user PAT if human, service PAT if agent/autopilot)
    → on 2xx: update cache, publish WS event, respond
    → webhook echo arrives later → idempotent, no-op re-apply

External change (someone edits in gitlab.com):
  gitlab.com → POST /api/gitlab/webhook
    → validate secret → enqueue → worker applies to cache + publishes WS event

Reconciliation (every 5min per connected workspace):
  server polls GitLab "issues updated since <last_sync_cursor>"
    → upsert into cache, publish WS events for drift found
```

### Components

- **`server/pkg/gitlab/`** — GitLab API client library (HTTP, pagination, rate-limit, retries). Token is injected per call.
- **`server/pkg/secrets/`** — AES-256-GCM wrapper for encrypting stored PATs.
- **`server/internal/gitlab/`** — Domain-aware layer: token resolver, webhook worker, reconciler, initial-sync worker, label bootstrap, Multica<->GitLab field translation.
- **`server/internal/handler/issue.go`** — refactored to the "call GitLab first, update cache on success" pattern.
- **`server/internal/handler/gitlab_webhook.go`** — webhook receiver endpoint.
- **`server/internal/handler/gitlab_connection.go`** — connect/disconnect endpoints (both workspace-admin and per-user).
- **Existing listeners** (`notification_listeners.go`, `activity_listeners.go`, `autopilot_listeners.go`) — largely unchanged; continue reading from the cache as they do today.

All new background work runs as goroutines in the same server binary/container. `SELECT ... FOR UPDATE SKIP LOCKED` on the webhook event table keeps workers independent.

## Data model

### New tables

#### `workspace_gitlab_connection`
Per-workspace config. One row per workspace that has GitLab connected.

| Column | Type | Notes |
|---|---|---|
| `workspace_id` | UUID, PK | FK → `workspaces` |
| `gitlab_project_id` | BIGINT | Numeric GitLab project ID |
| `gitlab_project_path` | TEXT | `group/project` display path; updated on webhook |
| `service_token_encrypted` | BYTEA | Workspace service PAT, AES-GCM encrypted |
| `service_token_user_id` | BIGINT | GitLab user ID the service PAT belongs to (for echo detection) |
| `webhook_secret` | TEXT | Random secret validated on every webhook delivery |
| `webhook_gitlab_id` | BIGINT | Hook ID GitLab returned when we created the webhook |
| `last_sync_cursor` | TIMESTAMP | Reconciliation cursor |
| `connection_status` | TEXT | `connecting` | `connected` | `error` |
| `status_message` | TEXT | Human-readable error when `connection_status='error'` |
| `created_at`, `updated_at` | TIMESTAMP | |

#### `user_gitlab_connection`
Per-user, per-workspace PAT for writes with correct attribution.

| Column | Type | Notes |
|---|---|---|
| `user_id` | UUID | FK → `users` |
| `workspace_id` | UUID | FK → `workspaces` |
| `gitlab_user_id` | BIGINT | GitLab user ID, captured from `/user` at registration |
| `gitlab_username` | TEXT | Cached for display |
| `pat_encrypted` | BYTEA | User's PAT, encrypted |
| `created_at` | TIMESTAMP | |
| PK | `(user_id, workspace_id)` | |

#### `gitlab_webhook_event`
Idempotency / dedupe for webhook deliveries.

| Column | Type | Notes |
|---|---|---|
| `id` | UUID, PK | |
| `workspace_id` | UUID | |
| `event_type` | TEXT | `issue`, `note`, `emoji`, `label` |
| `object_id` | BIGINT | GitLab IID / note ID / etc. |
| `gitlab_updated_at` | TIMESTAMP | |
| `payload_hash` | BYTEA | Hash of normalized event payload |
| `payload` | JSONB | Raw event for worker processing |
| `received_at` | TIMESTAMP | |
| `processed_at` | TIMESTAMP NULL | NULL until worker handles it |
| UNIQUE | `(workspace_id, event_type, object_id, payload_hash)` | |

#### `gitlab_label`
Cache of GitLab labels, keyed by GitLab label ID.

| Column | Type | Notes |
|---|---|---|
| `workspace_id` | UUID | |
| `gitlab_label_id` | BIGINT | |
| `name` | TEXT | |
| `color` | TEXT | Hex color from GitLab |
| `description` | TEXT | |
| `external_updated_at` | TIMESTAMP | |
| PK | `(workspace_id, gitlab_label_id)` | |

#### `issue_to_label`
Issue ↔ label association (replaces `issue_labels` + `issue_to_labels`).

| Column | Type | Notes |
|---|---|---|
| `issue_id` | UUID | FK → `issues.id` (the cache row) |
| `workspace_id` | UUID | Carried explicitly so the composite FK into `gitlab_label(workspace_id, gitlab_label_id)` is enforceable |
| `gitlab_label_id` | BIGINT | Part of composite FK → `gitlab_label(workspace_id, gitlab_label_id)` |
| PK | `(issue_id, gitlab_label_id)` | |
| FK | `(workspace_id, gitlab_label_id)` → `gitlab_label(workspace_id, gitlab_label_id)` | |

#### `gitlab_project_member`
Cache of GitLab project members for assignee picker, avatars, user-ID → display mapping.

| Column | Type | Notes |
|---|---|---|
| `workspace_id` | UUID | |
| `gitlab_user_id` | BIGINT | |
| `username` | TEXT | |
| `name` | TEXT | |
| `avatar_url` | TEXT | |
| `external_updated_at` | TIMESTAMP | |
| PK | `(workspace_id, gitlab_user_id)` | |

#### `issue_position`
Server-only ordering for drag-reorder (GitLab doesn't expose a clean per-issue global ordering). Rows are written on drag-reorder mutations. Issues without a row fall back to implicit ordering by `created_at DESC`.

| Column | Type | Notes |
|---|---|---|
| `workspace_id` | UUID | |
| `gitlab_iid` | INT | |
| `position` | NUMERIC | Fractional for cheap mid-sequence inserts |
| PK | `(workspace_id, gitlab_iid)` | |

#### `autopilot_issue`
Mapping from autopilot run → GitLab issue (replaces `issues.origin_type='autopilot'` / `origin_id`).

| Column | Type | Notes |
|---|---|---|
| `autopilot_run_id` | UUID | |
| `workspace_id` | UUID | |
| `gitlab_iid` | INT | |
| PK | `(autopilot_run_id, gitlab_iid)` | |

### Changes to existing tables

#### `issues` — reframed as cache
- **Add:** `gitlab_iid INT NOT NULL`, `gitlab_project_id BIGINT NOT NULL`, `external_updated_at TIMESTAMP`.
- **Drop:** `parent_issue_id`, `acceptance_criteria`, `context_refs`, `origin_type`, `origin_id`, `position`.
- **Keep:** `id`, `workspace_id`, `title`, `description`, `status`, `priority`, `assignee_type`, `assignee_id`, `creator_type`, `creator_id`, `due_date`, `created_at`, `updated_at` — all projected from GitLab on each webhook/read-through.
- **Unique:** `UNIQUE (workspace_id, gitlab_iid)`.

#### `comments` — reframed as cache
- **Add:** `gitlab_note_id BIGINT NOT NULL`, `external_updated_at TIMESTAMP`.
- **Drop:** comment types that no longer apply (`status_change`, `progress_update` — GitLab system notes cover these). Keep `comment` and `system`.
- **Unique:** `UNIQUE (workspace_id, gitlab_note_id)`.

#### `issue_reactions` — reframed as cache
- **Add:** `gitlab_award_id BIGINT NOT NULL`, `external_updated_at TIMESTAMP`.
- **Unique:** `UNIQUE (workspace_id, gitlab_award_id)`.

#### `attachments`
- Replace internal file storage with GitLab upload URLs. New column `gitlab_upload_url TEXT`; drop S3/internal storage FK columns.

### Tables dropped entirely

- `issue_labels` (old denormalized label definitions) — replaced by `gitlab_label`.
- `issue_to_labels` (old join table) — replaced by `issue_to_label` (singular, keyed by `gitlab_label_id`).
- `issue_dependencies` — feature dropped.
- `issue_subscribers` — replaced by GitLab's subscribe endpoint.

### Migration strategy

Single "squash-style" migration that drops removed columns/tables and adds new ones. No data preservation; workspaces are expected to be unpopulated. Aligns with CLAUDE.md's "no backwards-compat hacks for pre-launch replacements" rule.

## API surface

### Endpoints that stay (internal behavior changes)

All current `/api/issues/*` endpoints keep their URL + request/response shape. Internal changes:

- **Reads** (`GET /api/issues`, `GET /api/issues/{id}`, `GET /api/issues/search`, `GET /api/issues/{id}/comments`, `GET /api/issues/{id}/timeline`, `GET /api/issues/{id}/attachments`, `GET /api/issues/{id}/reactions`): unchanged — read from the cache.
- **Writes** (`POST /api/issues`, `PUT /api/issues/{id}`, `DELETE /api/issues/{id}`, `POST /api/issues/batch-update`, `POST /api/issues/batch-delete`, comment/reaction/subscribe writes): refactored to the "GitLab first, cache second" pattern (see below).

### Endpoints to remove

- `GET /api/issues/{id}/children` — sub-issues dropped.
- `GET|POST /api/issues/{id}/subscribe` — replaced by GitLab subscribe endpoint (exposed via a new handler that forwards to GitLab rather than reads from a table).

### New endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/workspaces/{id}/gitlab/connect` | Workspace admin provides service PAT + project; server validates, creates webhook, kicks off initial sync. |
| `DELETE` | `/api/workspaces/{id}/gitlab/connect` | Remove webhook in GitLab, delete connection row, cascade-truncate cache. |
| `GET` | `/api/workspaces/{id}/gitlab/connect` | Sanitized connection status (never returns the token). |
| `POST` | `/api/me/gitlab/connect` (workspace-scoped via header) | User registers their own PAT. |
| `DELETE` | `/api/me/gitlab/connect` | Remove user's PAT from current workspace. |
| `GET` | `/api/me/gitlab/connect` | Returns `{connected: bool, gitlab_username: string}`. |
| `POST` | `/api/gitlab/webhook` | Webhook receiver. No auth middleware; security is the `X-Gitlab-Token` secret. |

### GitLab client (`server/pkg/gitlab/`)

MVP methods: **issues** (list/get/create/update/close/reopen), **notes** (list/create/update/delete), **award emoji** (list/create/delete on issues and notes), **labels** (list/create/update/delete), **subscribe/unsubscribe**, **uploads**, **projects** (get/list members), **hooks** (create/update/delete), **user** (`/user`).

Cross-cutting behavior: rate-limit token bucket per GitLab token, honors `Retry-After` on 429, retries transient errors with exponential backoff (max 3 attempts), scrubs secrets from error messages via `server/pkg/redact/`, helpers for `Link: next` pagination. Client does NOT store the token — it is passed per call, enabling the hybrid auth model without ceremony.

### Write-path handler pattern

Every write handler follows this shape:

```
1. Load cached issue by Multica id → get gitlab_iid + project_id.
2. Translate Multica update → GitLab update (status/priority/agent-assignee → scoped labels; assignee member → GitLab assignee_ids; title/description/due_date → native fields).
3. Resolve the right token (see Auth).
4. Call GitLab.
5. On 2xx: apply the returned GitLab representation to the cache, publish the existing WS event, respond.
6. On 4xx/5xx: do NOT touch the cache; return a structured error.
```

`SearchIssues` stays on the cache — we explicitly do not route it to GitLab's search endpoint.

## Sync plane

### Initial sync (on connect)

1. `ListLabels(projectID)` → populate `gitlab_label`. Provision missing scoped labels we rely on: the full `status::*` and `priority::*` sets with fixed default colors. `agent::*` labels created lazily on first assignment.
2. Paginate `ListIssues(projectID, state=all, order_by=updated_at)` → upsert `issues` cache, with WS progress events.
3. For each issue (bounded parallelism 5): `ListNotes(iid)` + `ListAwardEmoji(iid)` → upsert `comments` and `issue_reactions`.
4. `ListProjectMembers(projectID)` → populate `gitlab_project_member`.
5. `CreateProjectHook(...)` with all needed events. Save `webhook_gitlab_id` + `webhook_secret`.
6. `last_sync_cursor = now()`; transition connection to `connected`.

Failures leave the connection in `connecting` with a retry button. Retry resumes from cursor.

### Webhook receiver

Endpoint: `POST /api/gitlab/webhook`. No auth middleware.

1. Match `X-Gitlab-Token` header against `workspace_gitlab_connection.webhook_secret` in constant time to identify the workspace. No match → 401.
2. Dispatch by `X-Gitlab-Event` (`Issue Hook`, `Note Hook`, `Emoji Hook`, `Label Hook`, etc.).
3. Compute payload hash, check dedupe table. Already processed → ACK 200 and stop.
4. Persist event to `gitlab_webhook_event` with `processed_at=NULL`, respond 200.
5. Worker pool picks up unprocessed rows via `SELECT ... FOR UPDATE SKIP LOCKED`, applies to cache transactionally, fires WS event, marks processed.

Ordering/idempotency: compare payload `gitlab_updated_at` against cache row's `external_updated_at`; skip if cache is newer.

#### Event → cache mapping

| GitLab event | Cache effect | WS event |
|---|---|---|
| `Issue Hook` open/update/close | Upsert `issues`, diff `issue_to_label`, resolve assignee (label-first, GitLab-assignee-second) | `issue:created` / `issue:updated` |
| `Note Hook` | Upsert `comments` | `comment:created` / `comment:updated` |
| `Emoji Hook` | Upsert `issue_reactions` | `issue_reaction:added` / `:removed` |
| `Label Hook` | Upsert/delete `gitlab_label` | `label:changed` (new event type) |

### Reconciler

Every 5 min per connected workspace:
- `ListIssues(updated_after=last_sync_cursor - 10m, order_by=updated_at, sort=asc)` — 10-min overlap window covers clock skew + in-flight webhooks.
- For each: create/update/skip based on `external_updated_at`.
- `ListLabels(projectID)` to catch label drift missed by label webhooks.
- Advance `last_sync_cursor = max(external_updated_at seen)`.
- If reconciler detects no webhooks received for >15 min despite writes, log warning and surface in connection status ("webhooks may be broken").

### Metrics (on existing `/metrics`)

`gitlab_webhook_received_total`, `gitlab_webhook_processing_lag_seconds`, `gitlab_reconcile_last_success_at`, `gitlab_api_rate_limit_remaining` — per workspace.

## Auth & secrets

### Encryption — `server/pkg/secrets/`

AES-256-GCM. Key from env var `MULTICA_SECRETS_KEY` (32 bytes, base64). Required at startup in non-dev mode. `make setup` generates a random dev key if unset. Format: `[12-byte nonce][ciphertext][16-byte GCM tag]`. Key rotation out of scope for MVP (add `key_version` prefix when needed).

Encrypted columns: `workspace_gitlab_connection.service_token_encrypted`, `user_gitlab_connection.pat_encrypted`. Nothing else is encrypted (webhook secret is random and only meaningful combined with the URL).

### PAT registration

**Workspace service PAT (admin, one-time):**
1. Admin provides project ID/path + service PAT.
2. Server calls `/user` with the PAT — captures GitLab user ID + username (stored as `service_token_user_id`).
3. Server calls `GetProject(projectID)` — validates access + `api` scope.
4. Encrypt PAT → `workspace_gitlab_connection` → kick off initial sync.

**Per-user PAT (each user, lazy):**
1. Banner prompts connection: *"Your writes will post as the service account until you connect."*
2. User enters PAT on their settings page.
3. Server calls `/user` → captures GitLab user ID + username → encrypts + stores in `user_gitlab_connection`.

**Required scope:** `api`.

### Token selection — `ResolveTokenForWrite`

```go
// server/internal/gitlab/token.go
func ResolveTokenForWrite(ctx context.Context, workspaceID uuid.UUID, actor Actor) (string, TokenOwner, error)
```

| Caller | Token used | GitLab attribution |
|---|---|---|
| Human with their PAT registered | User PAT | Their own GitLab account |
| Human without their PAT | Workspace service PAT | Service account (Multica still records the human via `activity_log`) |
| Agent (any) | Workspace service PAT | Service account (agent identity carried by `agent::<slug>` label + comment prefix) |
| Autopilot | Workspace service PAT | Service account (issues tagged `source::autopilot`) |

Reads use the cache and need no token. Initial sync and reconciler use the workspace service PAT.

### Failure modes

- Workspace service PAT revoked/expired → all writes return 502 `gitlab_service_auth_failed`. Webhook/reconciler stop. Admin sees reconnect banner.
- User PAT revoked → silent fallback to service PAT; one-time banner prompts reconnection.

### Sensitive value handling

- PATs never appear in logs (existing `server/pkg/redact/` handles `glpat-*` prefixes — verify coverage).
- PATs never appear in WS broadcasts.
- PATs never returned in API responses.

## Agents & autopilot

### Agent identity model

Source of truth for agent assignment is the `agent::<slug>` scoped label on the GitLab issue. The cache row (`assignee_type='agent'`, `assignee_id=<uuid>`) is derived from the label on webhook/read-through.

**Assigning an agent via Multica UI:**
1. Lookup agent slug from `agents` table.
2. Remove any existing `agent::*` label, add `agent::<slug>`, clear native GitLab assignee.
3. Write via the same token the general rule selects (user PAT if the acting human has one, service PAT otherwise) — the agent-assignment path does not get its own attribution exception.
4. Apply to cache, fire `issue:updated`.

**Webhook / read-through label parsing:**
- Exactly one `agent::<slug>` label → `assignee_type='agent'`, `assignee_id=<uuid>`.
- Otherwise, if a native GitLab assignee is set → map to Multica member via `user_gitlab_connection`, falling back to `gitlab_project_member` cache for GitLab-only users.
- Else → unassigned.

**Edge cases:**
- GitLab user assigned who isn't a Multica member → UI renders "Assigned: @gitlab-username (outside Multica)"; `assignee_id` is NULL in cache.
- Multiple `agent::` labels present → log warning, pick first alphabetically, surface UI banner on the issue.

### Assign-an-agent-from-GitLab

Because the agent assignment is the label, a human who adds `~"agent::builder"` from gitlab.com triggers the same dispatch path a Multica user would: webhook lands → cache updated → existing listener fires → agent task queue row created. This is a supported flow and documented in the agent SDK docs.

### Agent task queue

`agent_task_queue` table and daemon endpoints stay unchanged. Trigger moves to the cache update event, which is now driven by webhook or our own writes. Same listener logic either way.

### Agent writes

Agents continue to call the Multica REST API (unchanged SDK surface). Handlers transparently forward to GitLab using the workspace service PAT. Agent-authored comments get a prefix in the GitLab note body: `**[agent:<slug>]** <comment>`. Multica's UI strips the prefix on render (reading `comments.author_type='agent'` from the cache), GitLab preserves it as the readable source.

### Autopilot

- Column drops: `issues.origin_type`, `issues.origin_id`. Every issue is now implicitly a GitLab issue; no origin column needed.
- New mapping table: `autopilot_issue` (schema above).
- Autopilot's create path: `gitlab.CreateIssue` via service PAT, insert `autopilot_issue` mapping, let webhook fill cache. Generated issues get `source::autopilot` scoped label (convenience for humans browsing GitLab).
- Autopilot listener (`server/cmd/server/autopilot_listeners.go`): joins `autopilot_issue` to find issues it cares about, rather than filtering on `origin_type`.

### Agents acting outside Multica

Agents calling GitLab directly (bypassing Multica) land in the cache whenever the webhook arrives. Same eventual consistency as any other gitlab.com-side edit. Noted in agent SDK docs.

## Frontend

### New flows (shared: `packages/views/`)

- **Connect GitLab (workspace settings)** — `packages/views/workspace/settings/gitlab/`. States: not-connected (project ID + service PAT form), connecting (progress bar fed by WS), connected (project info + disconnect), error (retry).
- **Per-user PAT banner + settings** — Banner in workspace chrome when the user's PAT isn't registered: *"Your writes will post as the service account. Connect your GitLab to attribute actions to you."* Dismissable per workspace. Settings at `packages/views/user/settings/gitlab/`.

### Changes to existing views

- **Issue list / detail / kanban:** largely unchanged (still read cache). Label chips now render `gitlab_label.color`. Assignee slot gains a "not in Multica" variant for GitLab-only users.
- **Removed UIs:** sub-issues panel, dependencies panel, acceptance-criteria editor, context-refs editor — all removed.
- **Search:** unchanged (still hits `/api/issues/search` → cache).

### Stores (`packages/core/`)

- **New:** `gitlab-connection-store.ts` (per-workspace connection status); `user-gitlab-connection-store.ts` (user PAT state — boolean + display username only, never the token).
- **Removed:** sub-issue, dependency, acceptance-criteria, context-refs stores and their supporting plumbing.

### WS events

New broadcast types added to the existing WS client plumbing: `gitlab_connection:updated`, `gitlab_sync:progress`, `label:changed`. No changes to the underlying transport.

## Testing

| Layer | What | How |
|---|---|---|
| `server/pkg/gitlab/` | Client methods, rate limiting, pagination, retries, error scrubbing | Go unit tests against `httptest.Server` fixtures captured from real GitLab responses |
| `server/internal/handler/issue.go` | Write-path fan-out, cache updated on success, left alone on failure, token selection | Handler tests with injected fake `GitLabClient` interface |
| Webhook receiver | Secret validation, dedupe, event → cache mapping, idempotency ordering | Go tests with captured real GitLab webhook payloads |
| Reconciler | Drift pickup, cursor advancement, 429 handling | Go tests with fake client |
| `packages/core/` | New connection stores | Vitest |
| `packages/views/workspace/settings/gitlab/` | Connect flow + error states | Vitest + Testing Library |
| E2E | Connect → sync → create issue → webhook echo → assign agent → agent picks up | Playwright against a dedicated throwaway GitLab.com test project (service PAT in CI secrets) |

Fixture strategy: capture real GitLab API responses and webhook payloads once, commit as test data. Cheaper and more realistic than hand-writing JSON.

## Decomposition into implementation plans

This spec is too big for a single plan. Five sub-projects in dependency order — each gets its own plan → implementation cycle:

1. **Foundation.** `server/pkg/secrets/`, `GitLabClient` library, new connection tables, connect/disconnect endpoints, workspace-admin connect-GitLab UI. Ships: workspace admin can connect and the service PAT is validated against GitLab; no issue flow wired yet. **Small-medium.**
2. **Read path.** Cache schema migration, initial sync worker, webhook receiver + worker, reconciler. Ships: issues appear read-only in Multica after connect. Write endpoints temporarily return 501. **Large.**
3. **Write path.** Every write handler refactored to "GitLab first, cache second." Per-user PAT flow + banner. Token resolver. Agent-as-label write mapping. Ships: feature-complete for human users. **Medium-large.**
4. **Agents + autopilot re-point.** Agent task queue against cache updates, comment prefix convention, autopilot creates GitLab issues + `autopilot_issue` mapping, "assign agent via label in GitLab" dispatch. Ships: agents and autopilot end-to-end. **Medium.**
5. **Cleanup.** Delete dead handlers, tables, frontend views (sub-issues, dependencies, acceptance-criteria, context-refs, old origin columns, old label tables). **Small.**

Each phase is independently shippable (or passes `make check`). The only visible regression window is in phase 2 (writes disabled); the connect flow is gated behind a feature flag until phase 3 ships.

## Open questions / things deliberately left unresolved

- **Key rotation / KMS.** Noted as future work. MVP uses a single env-var key.
- **Multi-instance server scale-out.** Reconciler would need `pg_advisory_lock` or leader election. Not built now.
- **Self-hosted GitLab.** Explicitly out of scope. Base URL is hardcoded to `https://gitlab.com`.
- **OAuth.** PAT-only for v1. OAuth with refresh tokens is a natural v2.
- **GitLab Workspaces / merge-request integration.** Out of scope. This spec covers issues only.
