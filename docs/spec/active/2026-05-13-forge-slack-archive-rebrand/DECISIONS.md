---
document_type: decisions
project_id: SPEC-2026-05-13-001
---

# Forge: Slack + Archive + Tab Rebrand — Architecture Decision Records

## ADR-001: Workspace-scoped Slack config, not per-user

**Date**: 2026-05-13
**Status**: Accepted
**Deciders**: shivasymbl, claude-sonnet-4.6, gpt-5.4 (zen analysis)

### Context

The Forge backend already has a `NotificationPreferences` table at user-workspace granularity (`notification_preference.go`). The naive shortcut would be to extend that table with a `slack_webhook` column or add Slack to `notifTypeToGroup`. The question is whether Slack config belongs to user preferences or workspace integrations.

### Decision

Slack webhook config lives in a new `workspace_slack_integrations` table, scoped to a workspace. It is NOT modeled as a user notification preference.

### Consequences

**Positive:**
- Conceptually clean: user preferences are recipient-side ("don't notify ME for X"); webhooks are routing-side ("for the workspace, send X events to this channel")
- Admin-only configuration matches Slack channel governance (channels are team-owned, not personal)
- Future Teams/Discord/email-digest integrations slot into the same workspace_integrations pattern
- Doesn't pollute the existing per-user notification model

**Negative:**
- Two notification surfaces to maintain (inbox + Slack) instead of one
- Users with personal Slack accounts can't get DMs without a future bot-token feature

**Neutral:**
- DB has one new table

### Alternatives Considered

1. **Extend `notification_preferences` with `slack_webhook` column** — Rejected. Per-user webhook URLs are operationally a nightmare (every member would need to set up their own webhook). Slack incoming webhooks point at a channel, not a user — so the user-scoped model doesn't match the technical reality.
2. **Single `workspace_integrations` table covering Slack, Teams, GitHub, future** — Rejected for MVP. The GitHub integration already exists with its own table (`github_installations`). Forcing a generic schema now is premature abstraction. Plural-friendly Slack table allows generification later.

### Evidence

- `server/internal/handler/notification_preference.go` — existing per-user model
- `server/cmd/server/notification_listeners.go:80` — `notifTypeToGroup` is a user-preference filter, irrelevant to workspace routing
- Slack docs: incoming webhooks bind to one channel: https://api.slack.com/messaging/webhooks
- zen analysis: "Workspace webhooks are a different notification domain than user inbox preferences"

---

## ADR-002: Async fire-and-forget delivery

**Date**: 2026-05-13
**Status**: Accepted
**Deciders**: shivasymbl, claude-sonnet-4.6

### Context

The Forge event bus (`server/internal/events/bus.go`) dispatches events synchronously within the goroutine that called `Publish`. In practice this means status-change events run inside the HTTP request goroutine handling `PATCH /api/issues/:id`. If a listener does a slow HTTP call to Slack, the issue update API call blocks waiting for Slack.

### Decision

Slack POST is fired in a goroutine spawned inside `slack.NotifyStatusChange`. The listener returns immediately to the bus. The goroutine has a 5-second HTTP timeout and a recover() against panics.

### Consequences

**Positive:**
- Issue update HTTP latency is unaffected by Slack RTT or Slack outage
- Slack 5xx / timeouts do not appear as API errors to users
- Bounded resource use (single goroutine per status change with 5s ceiling)

**Negative:**
- No delivery guarantee — if the server crashes between event and POST, the message is lost
- No retries (v1) — a transient Slack 503 means the notification is missed

**Neutral:**
- Loss is acceptable for v1: the inbox notification is the source of truth, Slack is a "ping" channel

### Alternatives Considered

1. **Synchronous POST inside listener** — Rejected. Couples Forge API latency to Slack health.
2. **Queue-based delivery (Redis stream → worker)** — Rejected for MVP. Adds infrastructure complexity for an extreme edge case (Slack outage during status change). Defer until we see actual loss.
3. **Use Go's `time.AfterFunc` retry loop** — Rejected. Stateful retries in process = lost on restart anyway.

---

## ADR-003: New Go package `internal/integrations/slack/`

**Date**: 2026-05-13
**Status**: Accepted
**Deciders**: shivasymbl, gpt-5.4

### Context

We can put Slack code either (a) inline in `notification_listeners.go` (extending the existing file), or (b) in a new isolated package `internal/integrations/slack/`. The fork has historically suffered from upstream merge conflicts when feature code lives inline in dense existing files.

### Decision

All Slack logic lives in `server/internal/integrations/slack/` as a new package. The only edit to existing files is a 5-line addition in `notification_listeners.go` to call `slack.NotifyStatusChange(...)`.

### Consequences

**Positive:**
- Upstream changes to `notification_listeners.go` rarely touch our 5-line hook
- Zero merge conflict for the bulk of Slack code (it's in a Forge-only path)
- Easy to swap out / replace if upstream ships a competing Slack feature

**Negative:**
- Slightly more files to navigate (6 new files vs 1 modified file)

### Evidence

- v0.2.32 sync experience: `notification_listeners.go` had upstream changes that overlapped with our RBAC patch and required manual conflict resolution
- Pattern matches: `server/internal/cli/` (Forge-only), `server/internal/integrations/` (new top-level)

---

## ADR-004: One webhook per workspace (MVP), table supports many

**Date**: 2026-05-13
**Status**: Accepted

### Context

Some teams want multiple Slack channels (e.g. #engineering for `in_progress`, #ops for `done`). MVP scope is one channel per workspace; the question is whether to enforce that in the schema or just in the UI.

### Decision

DB schema is plural-friendly: `workspace_slack_integrations` table has no `UNIQUE workspace_id` constraint on its own. The partial unique index `WHERE enabled = true` ensures at most one active integration per workspace, but multiple disabled rows are allowed (history) and the constraint can be relaxed without a migration.

### Consequences

**Positive:**
- v2 "multiple channels per workspace" doesn't require a migration
- Disabled rows preserve history (you can re-enable instead of re-paste URL)

**Negative:**
- Minor schema complexity vs. a simple `workspace_settings` column

---

## ADR-005: `cancelled` DB value preserved; only UI relabels to "Archive"

**Date**: 2026-05-13
**Status**: Accepted
**Deciders**: shivasymbl, gpt-5.4

### Context

User wants the `Cancelled` status renamed to `Archive`. Options:
- A) Change UI label only — DB value stays `cancelled`
- B) Add `archive` as a new status, migrate existing `cancelled` rows
- C) Rename the DB enum value `cancelled` → `archive`

jcodemunch blast radius analysis: `IssueStatus` touches 79 files. The DB `CHECK` constraint hardcodes the 7 values. Go handler has 9 hardcoded status comparisons. The agent dispatch logic depends on specific status values.

### Decision

Option A: relabel in UI only. The DB value stays `cancelled`. The display string in 5 files changes to "Archive".

### Consequences

**Positive:**
- 5-file change with zero schema impact
- Zero risk to agent dispatch, task cancellation, terminal-status logic
- Zero upstream merge conflict (we just override display strings; upstream's status semantics unchanged)
- Reversible by changing one string

**Negative:**
- Mismatch between API status value (`cancelled`) and UI label (`Archive`) — slight confusion for API users
- Migration to a true "Archive" status (if ever wanted) is a separate project

### Alternatives Considered

1. **Add `archive` as new status, migrate `cancelled` rows** — Rejected. Requires: DB migration with `CHECK` constraint change, updating `BOARD_STATUSES`, `STATUS_CONFIG`, `STATUS_ORDER`, `ALL_STATUSES`, `statusLabels` map (Go), `terminalStatusForTaskFailedDismiss` map, 9 hardcoded comparisons in `issue.go`, agent dispatch logic, plus 79 TS files transitively. High risk of upstream conflict.
2. **Rename enum value `cancelled` → `archive`** — Rejected. Same 79-file blast radius plus a complex data migration (UPDATE all rows, then drop/recreate CHECK constraint). Upstream will likely keep `cancelled` so every future sync is painful.
3. **Workspace-custom statuses (true configurability)** — Rejected. Per zen analysis, this is a "workflow engine" feature requiring re-modeling status semantics across the entire backend. Defer until upstream's roadmap is clearer.

### Evidence

- jcodemunch blast radius: `IssueStatus` = 79 files, risk score 0.59
- `BOARD_STATUSES` already excludes `cancelled` — `cancelled` is already board-hidden
- `terminalStatusForTaskFailedDismiss` already treats `cancelled` as terminal
- `issue.go:1593` already cancels running agent tasks when status becomes `cancelled`
- Semantically: `cancelled` already IS archive behavior — only the label is wrong

---

## ADR-006: Plain text Slack message in v1; Block Kit deferred

**Date**: 2026-05-13
**Status**: Accepted

### Context

Slack supports two message formats: plain text (`{"text": "..."}`) and Block Kit (`{"blocks": [...]}`). Block Kit gives richer cards with status pills, buttons, etc.

### Decision

v1 uses plain text with light Slack mrkdwn formatting. v2 may upgrade to Block Kit.

### Consequences

**Positive:**
- Simpler to implement, test, and debug
- Less coupling to Slack's Block Kit schema (less to break if Slack changes)

**Negative:**
- Less visually polished

### Alternatives Considered

1. **Block Kit from day 1** — Rejected. Adds Slack-specific schema knowledge, more test surface, no functional win for v1.

---

## ADR-007: Webhook URL stored plaintext

**Date**: 2026-05-13
**Status**: Accepted

### Context

The webhook URL IS the secret — anyone with it can post arbitrary messages to the configured Slack channel. The question is whether to encrypt at rest.

### Decision

Webhook URL stored as plaintext TEXT in the DB column. Not encrypted.

### Consequences

**Positive:**
- No KMS / HSM infrastructure to manage
- No key rotation complexity
- Simple to implement and debug

**Negative:**
- DB dump exposure leaks all workspace Slack webhooks
- Database-level access = full Slack channel write access

### Risk Acceptance

If an attacker has access to the Postgres database, they already have:
- Every issue, every comment, every project
- Every user's auth token (in `users.password` / session tokens)
- Every agent runtime token

The webhook URL is one of dozens of secrets in the DB. Encrypting it without an HSM is theatre — the encryption key has to be readable by the running server, which means the same compromise that reads the DB also reads the key.

Encryption with proper HSM/KMS infra is a project-wide concern, not a Slack-specific one. Address holistically (or not at all) in a future security hardening phase.

### Mitigations Still Required

- URL never returned in plain text in API responses (always masked)
- URL never logged in plain text
- SSRF prevention: URL must start with `https://hooks.slack.com/services/`
- URL never published over WebSocket events

---

## ADR-008: Tab rebrand done as part of this PR, not deferred

**Date**: 2026-05-13
**Status**: Accepted

### Context

The browser tab brand leak (Multica favicon + title) was missed in the v0.2.32 sync. We can either:
- A) Ship as part of this Slack PR (bundled)
- B) Ship as a separate hotfix

### Decision

Bundle in this PR. The change is 2 files, mechanical, and the PR already includes brand-related changes (Archive relabel). Single deploy is simpler.

### Consequences

**Positive:**
- One deploy instead of two
- All brand polish in one merge ref for `verify-patches.sh` to gate
- Single CI run

**Negative:**
- Slightly larger PR (but still focused — all 3 changes are post-sync polish)
