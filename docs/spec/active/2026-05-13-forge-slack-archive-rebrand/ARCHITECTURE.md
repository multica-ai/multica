---
document_type: architecture
project_id: SPEC-2026-05-13-001
version: 1.0.0
last_updated: 2026-05-13T21:50:00+05:30
status: in-review
---

# Forge: Slack + Archive + Tab Rebrand — Technical Architecture

## System Overview

Three additive changes layered on top of Forge v0.2.32. Architecture is dominated by the Slack integration; the other two are pure renames/replacements.

### High-level diagram

```
                           ┌──────────────────────────┐
                           │  Issue Update HTTP Req   │
                           │  (PATCH /api/issues/:id) │
                           └──────────────┬───────────┘
                                          │
                                          ▼
                       ┌────────────────────────────────┐
                       │  server/internal/handler       │
                       │  /issue.go: UpdateIssue        │
                       │   - writes DB                  │
                       │   - publishes EventIssueUpdated│
                       └─────────────┬──────────────────┘
                                     │
                                     ▼
                       ┌────────────────────────────────┐
                       │  server/internal/events/bus.go │
                       │  bus.Publish(EventIssueUpdated)│
                       │  (synchronous dispatch)        │
                       └─┬──────────────┬───────────┬───┘
                         │              │           │
                ┌────────▼─────┐ ┌──────▼──────┐ ┌──▼─────────────────┐
                │ notification │ │  activity   │ │ subscriber         │
                │ _listeners.go│ │  _listeners │ │  _listeners.go     │
                │  (existing)  │ │  (existing) │ │  (existing)        │
                └──────┬───────┘ └─────────────┘ └────────────────────┘
                       │
                       │  (NEW: 5-line addition inside `if statusChanged` block)
                       │
                       ▼
                ┌──────────────────────────────────────┐
                │ server/internal/integrations/slack   │
                │  notify.go: NotifySlackOnStatusChange│
                │   - go func() { ... }()  (async)     │
                └───────────────┬──────────────────────┘
                                │
                                ▼
                  ┌─────────────────────────────────┐
                  │ DB: workspace_slack_integrations│
                  │  - load config for workspace    │
                  │  - check status in trigger set  │
                  └────────────┬────────────────────┘
                               │
                               ▼
                  ┌─────────────────────────────────┐
                  │ slack/format.go: Build payload  │
                  │  (plain text / Block Kit)       │
                  └────────────┬────────────────────┘
                               │
                               ▼ (HTTP POST, 5s timeout)
                  ┌─────────────────────────────────┐
                  │ Slack Incoming Webhook URL      │
                  │  hooks.slack.com/services/...   │
                  └─────────────────────────────────┘
```

### Key design decisions

| Decision | Rationale | ADR |
|---|---|---|
| Workspace-scoped Slack config, not per-user | Slack channel routing is integration policy, not recipient preference | ADR-001 |
| Async fire-and-forget delivery | Event bus dispatches synchronously in HTTP request goroutine; blocking would add Slack RTT to every issue update | ADR-002 |
| New Go package `internal/integrations/slack/` | Isolates upstream merge conflict surface to one 5-line hook in `notification_listeners.go` | ADR-003 |
| One webhook per workspace (MVP); table supports many | Plural-friendly table structure means v2 can add more without migration | ADR-004 |
| `cancelled` status value preserved in DB | Renaming the value would have 79-file TS + 9 Go-line + 1 migration blast radius and would clash with upstream | ADR-005 |
| Block Kit deferred to P2 | Plain text works, Block Kit is polish | ADR-006 |
| Webhook URL not encrypted | Already gated by DB access; encryption is theatre without an HSM | ADR-007 |

## Component Design

### Component 1: `server/internal/integrations/slack/` (new package)

**Purpose**: Encapsulate all Slack-specific logic in one place so upstream merges don't touch this code.

**Files**:

| File | Purpose | LOC estimate |
|---|---|---|
| `notify.go` | `NotifyStatusChange(ctx, queries, evt)` — the listener entrypoint | ~80 |
| `format.go` | Format the message body (plain text v1, Block Kit later) | ~50 |
| `client.go` | HTTP POST with timeout, SSRF validation | ~40 |
| `notify_test.go` | Unit tests for trigger filtering | ~120 |
| `format_test.go` | Unit tests for message body | ~60 |
| `client_test.go` | Unit tests for HTTP layer (httptest.Server) | ~80 |

**Interfaces**:

```go
package slack

// NotifyStatusChange is the listener invoked from notification_listeners.go
// when an issue's status has changed. Loads workspace config, filters by
// trigger statuses, formats message, posts to Slack — all async.
func NotifyStatusChange(
    ctx context.Context,
    queries *db.Queries,
    workspaceID string,
    issue handler.IssueResponse,
    prevStatus string,
    actorName string,
) {
    go func() {
        // 1. Load workspace_slack_integrations row
        // 2. Check enabled + status in trigger set
        // 3. Build message
        // 4. POST with 5s timeout
        // 5. Log success/failure
    }()
}
```

**Dependencies**:
- `server/pkg/db/generated` (sqlc-generated queries)
- `server/internal/handler` (IssueResponse type — read-only)
- `net/http` (stdlib)
- `log/slog` (stdlib)

### Component 2: `notification_listeners.go` hook (existing file, +5 lines)

**Location**: `server/cmd/server/notification_listeners.go` line ~654, inside the `if statusChanged {` block of the `protocol.EventIssueUpdated` subscriber.

**Change**:

```go
if statusChanged {
    // ... existing inbox notification logic ...

    // NEW: dispatch to Slack integration (async, isolated)
    actorName := resolveActorName(ctx, queries, e.ActorType, e.ActorID)
    slack.NotifyStatusChange(ctx, queries, issue.WorkspaceID, issue, prevStatus, actorName)
}
```

**Why here**: Single hook point, already has all the data (issue, prevStatus, actor, queries). Future upstream additions to this block won't conflict with our single-line additive call.

### Component 3: HTTP handler — `server/internal/handler/slack_integration.go` (new file)

**Routes** (admin-only, RBAC-gated in router):

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/workspaces/{id}/integrations/slack` | Read current config (URL masked) |
| `PUT` | `/api/workspaces/{id}/integrations/slack` | Create or update config |
| `DELETE` | `/api/workspaces/{id}/integrations/slack` | Disable + clear config |
| `POST` | `/api/workspaces/{id}/integrations/slack/test` | Fire a synthetic "Test message from Forge" |

**RBAC**: All four routes wrapped with `middleware.RequireWorkspaceRole(queries, "owner", "admin")` — same pattern as `router.go:447` for runtime management. Members get 403.

**Response shape**:

```go
type SlackIntegrationResponse struct {
    ID              string   `json:"id"`
    WorkspaceID     string   `json:"workspace_id"`
    Enabled         bool     `json:"enabled"`
    WebhookURLMasked string  `json:"webhook_url_masked"`  // "••••••••XYZ123"
    TriggerStatuses []string `json:"trigger_statuses"`
    Label           string   `json:"label"`
    CreatedAt       string   `json:"created_at"`
    UpdatedAt       string   `json:"updated_at"`
}
```

The raw webhook URL is never returned in any GET response — only the masked version.

### Component 4: `integrations-tab.tsx` Slack card (existing file, +card)

**Location**: `packages/views/settings/components/integrations-tab.tsx` — add a second `<Card>` immediately after the existing GitHub `<Card>`.

**Structure** (mirrors GitHub card):

```tsx
<Card>
  <CardContent className="space-y-4">
    <div className="flex items-start justify-between gap-4">
      <div className="flex items-start gap-3">
        <SlackMark className="h-6 w-6 mt-0.5 shrink-0" />
        <div className="space-y-1">
          <p className="text-sm font-medium">Slack</p>
          <p className="text-xs text-muted-foreground">
            Post to a Slack channel when issues change status. Uses an
            Incoming Webhook URL.
          </p>
        </div>
      </div>
      {canManage && <Button size="sm" onClick={save}>Save</Button>}
    </div>

    {canManage ? (
      <>
        <Input
          type="password"
          placeholder="https://hooks.slack.com/services/..."
          value={config?.webhook_url_masked ?? draftUrl}
          onChange={(e) => setDraftUrl(e.target.value)}
        />
        <div>
          <p className="text-xs font-medium mb-2">Notify when status changes to:</p>
          <div className="flex flex-wrap gap-3">
            {ALL_STATUSES.map((s) => (
              <label key={s} className="flex items-center gap-1.5 text-xs">
                <Checkbox checked={triggers.includes(s)} onChange={...} />
                {STATUS_CONFIG[s].label}
              </label>
            ))}
          </div>
        </div>
        <div className="flex items-center justify-between pt-2">
          <Switch checked={config?.enabled} onCheckedChange={toggle} />
          <Button variant="outline" size="sm" onClick={testFire}>Send test</Button>
        </div>
      </>
    ) : (
      <p className="text-xs text-muted-foreground">{t.manage_hint}</p>
    )}
  </CardContent>
</Card>
```

### Component 5: Tab rebrand (existing files, edits only)

| File | Change |
|---|---|
| `apps/web/app/layout.tsx:76` | `metadataBase: new URL("https://forge.asymbl.app")` |
| `apps/web/app/layout.tsx:78` | `default: "Forge — Asymbl's AI-native project workspace"` |
| `apps/web/app/layout.tsx:79` | `template: "%s | Forge"` |
| `apps/web/app/layout.tsx:81` | description: Asymbl Forge wording |
| `apps/web/app/layout.tsx:89` | `siteName: "Forge"` |
| `apps/web/app/layout.tsx:94-95` | twitter: `@asymbl` (or remove if no handle) |
| `apps/web/public/favicon.svg` | Replace Multica asterisk SVG with Asymbl bracket+dot SVG (or simply delete to fall through to `/brand/favicon.png`) |
| `apps/web/app/(landing)/layout.tsx:24,30` | "Multica" → "Forge" |

### Component 6: Archive relabel (existing files, edits only)

| File | Change | Line |
|---|---|---|
| `packages/core/issues/config/status.ts` | `label: "Cancelled"` → `"Archive"` | 49 |
| `packages/views/locales/en/issues.json` | `"cancelled": "Cancelled"` → `"Archive"` | 16 |
| `packages/views/locales/en/issues.json` | `"status_cancelled": "Cancelled"` → `"Archive"` | 240 |
| `packages/views/locales/en/projects.json` | `"cancelled": "Cancelled"` → `"Archive"` | 21 |
| `server/cmd/server/notification_listeners.go` | `"cancelled": "Cancelled"` → `"Archive"` (notification text) | 31 |
| `packages/views/locales/zh-Hans/issues.json` | Match (if upstream zh has "Cancelled") | search |
| `packages/views/locales/zh-Hans/projects.json` | Match | search |

The DB column value stays `cancelled`. No migration. No API change.

## Data Design

### Data Models

```sql
-- server/migrations/089_workspace_slack_integrations.up.sql
CREATE TABLE workspace_slack_integrations (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    enabled           BOOLEAN NOT NULL DEFAULT true,
    webhook_url       TEXT NOT NULL,
    label             TEXT NOT NULL DEFAULT 'Slack',
    -- JSON-encoded array of status strings, e.g.
    --   ["in_progress","in_review","done","blocked"]
    -- Empty array means "fire on all statuses".
    trigger_statuses  JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_sent_at      TIMESTAMPTZ,
    last_error        TEXT,
    created_by        UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Only one active config per workspace for MVP (UI enforces, DB enforces).
-- Inactive rows preserved so the user can re-enable without losing config.
CREATE UNIQUE INDEX idx_workspace_slack_enabled
  ON workspace_slack_integrations (workspace_id)
  WHERE enabled = true;

-- Lookups always go by workspace_id.
CREATE INDEX idx_workspace_slack_workspace ON workspace_slack_integrations (workspace_id);
```

```sql
-- server/migrations/089_workspace_slack_integrations.down.sql
DROP INDEX IF EXISTS idx_workspace_slack_enabled;
DROP INDEX IF EXISTS idx_workspace_slack_workspace;
DROP TABLE IF EXISTS workspace_slack_integrations;
```

### Data Flow

1. **Issue update** lands in `handler.UpdateIssue`
2. Handler writes DB, then `bus.Publish(EventIssueUpdated)` with payload including `statusChanged bool`
3. `notification_listeners.go` subscriber sees `statusChanged = true`, runs existing inbox notification logic
4. **NEW**: Same subscriber calls `slack.NotifyStatusChange(...)` (synchronous call into our package)
5. `slack.NotifyStatusChange` immediately spawns `go func() { ... }()` and returns
6. Goroutine: loads `workspace_slack_integrations` row, filters by `enabled` and `trigger_statuses`, formats message, HTTP POST with 5s timeout
7. On success: update `last_sent_at`. On failure: update `last_error`, log

### Storage Strategy

- One Postgres table. Existing pgvector image. No new extensions.
- No caching needed (single-row read per status change, indexed).

## API Design

### Endpoints

| Method | Path | Auth | Purpose | Request body | Response |
|---|---|---|---|---|---|
| `GET` | `/api/workspaces/{wsId}/integrations/slack` | admin/owner | Read config | — | `SlackIntegrationResponse` (URL masked) |
| `PUT` | `/api/workspaces/{wsId}/integrations/slack` | admin/owner | Create/update | `{webhook_url, trigger_statuses, label, enabled}` | `SlackIntegrationResponse` |
| `DELETE` | `/api/workspaces/{wsId}/integrations/slack` | admin/owner | Disable | — | 204 |
| `POST` | `/api/workspaces/{wsId}/integrations/slack/test` | admin/owner | Send test message | — | `{ok: true}` or 502 |

### Validation rules

- `webhook_url` must start with `https://hooks.slack.com/services/` (SSRF prevention)
- `trigger_statuses` must be a JSON array; each element must be one of the 7 known status values

## Integration Points

### Internal Integrations

| Component | How |
|---|---|
| Event bus | Subscribe via `notification_listeners.go`, single line addition |
| sqlc queries | New file `server/pkg/db/queries/slack.sql` |
| RBAC middleware | Reuse `middleware.RequireWorkspaceRole(queries, "owner", "admin")` |
| Settings UI | Add card to `integrations-tab.tsx`, reuses `useT`, `useQuery`, `toast` |

### External Integrations

| Service | Integration |
|---|---|
| Slack | Incoming Webhooks (POST application/json to `https://hooks.slack.com/services/T.../B.../X...`) |

## Security Design

### Authentication

- All routes go through the existing auth middleware (`/api/workspaces/...` is already auth-required).

### Authorization

- All write routes (PUT, DELETE, POST /test) require workspace `owner` or `admin` role.
- GET allowed for admin/owner too (URL is sensitive even masked).

### Data Protection

- Webhook URL stored plaintext in DB column. Risk-accepted in ADR-007 (DB access ≈ full compromise; encryption is theatre without HSM).
- URL never returned in GET response — only masked (`••••••••XYZ123`).
- SSRF prevention: URL must start with `https://hooks.slack.com/services/`. Reject everything else server-side.

### Threat model

| Threat | Mitigation |
|---|---|
| SSRF via webhook URL (attacker sets URL to `http://169.254.169.254/...`) | Server validates `https://hooks.slack.com/services/` prefix |
| URL leakage via API response | Always masked on GET; never logged in plaintext |
| URL leakage via WS event | Slack config is never published over WS |
| Slack rate-limit DoS against Forge | 5s timeout per POST, no retries — bounded resource use |
| Replay attack on `/test` route | Admin-only + CSRF protection inherited from existing API |

## Performance Considerations

### Expected Load

- 5 workspaces × ~10 status changes/day = ~50 Slack POSTs/day total. Negligible.
- Goroutine overhead per status change: ~4KB stack, sub-millisecond.

### Performance Targets

| Metric | Target |
|---|---|
| Issue update HTTP latency added by Slack | 0ms (fully async) |
| Slack POST end-to-end | < 5s (timeout enforced) |
| Settings panel load | < 100ms p95 |

## Reliability & Operations

### Availability Target

- Slack failures DO NOT affect Forge availability. Slack downtime → logged warnings, no user-visible impact beyond missing Slack messages.

### Failure Modes

| Failure | Impact | Recovery |
|---|---|---|
| Slack returns 5xx | Skipped, logged | Auto: next status change retries naturally |
| Slack returns 4xx (bad URL) | Skipped, `last_error` populated | Manual: admin fixes URL |
| Slack returns 429 (rate-limited) | Skipped, logged | Auto: next attempt likely fine; v2 may add backoff |
| Goroutine panic | Recovered by event bus (already does this) | Auto: logged, next event fine |
| DB query for config fails | Skipped, logged | Auto: next attempt retries |

### Monitoring & Alerting

- `last_sent_at` and `last_error` columns serve as in-app monitoring (visible on settings card).
- `slog.Warn` on failure with workspace_id + status code.
- No new metrics required for MVP.

## Testing Strategy

See `IMPLEMENTATION_PLAN.md` for the full test-class breakdown. Summary:

### Unit Testing

- `slack/notify_test.go` — trigger status filtering, enabled/disabled gate
- `slack/format_test.go` — message body contains all required fields
- `slack/client_test.go` — HTTP timeout, SSRF rejection, 200/4xx/5xx handling

### Integration Testing

- `slack_integration_test.go` — end-to-end via mock Slack server: status change → POST → message received
- `slack_integration_failure_test.go` — Slack returns 500 → issue update still returns 200, error logged

### Frontend Testing

- `integrations-tab.test.tsx` — Slack card renders, admin can save, member sees read-only
- Test webhook URL masking display

### E2E Testing

- Deferred. Manual smoke test on staging covers it for v1.

## Deployment Considerations

### Environment Requirements

- No new env vars required. Webhook URL stored per workspace.

### Configuration Management

- Slack integration config is workspace data, lives in app DB.
- No `forge/prd` Doppler change needed.

### Rollout Strategy

1. Merge to `main` → Depot CI builds new backend + frontend images
2. Deploy workflow runs migration `089_workspace_slack_integrations.up.sql` automatically via `entrypoint.sh`
3. Containers restart with new images
4. Smoke test: visit Settings → Integrations on forge.asymbl.app

### Rollback Plan

- Revert main commit + push → Depot redeploys old image (sha-8bd9850)
- Migration is additive only; old image's `./migrate up` is a no-op
- If migration rollback needed: `migrate -path server/migrations -database $DATABASE_URL down 1`

## Future Considerations

- Multiple webhooks per workspace (table already supports it)
- Slack Bot Token integration for DM-to-user (different package, same hook point)
- Microsoft Teams webhook integration (same listener pattern, new package)
- Email digest of status changes (different cadence; daily summary)
- Generic outbound webhook (any URL, any event) — for Zapier/Make integrations
