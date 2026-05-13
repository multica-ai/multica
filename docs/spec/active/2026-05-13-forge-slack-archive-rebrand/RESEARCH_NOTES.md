---
document_type: research
project_id: SPEC-2026-05-13-001
last_updated: 2026-05-13T21:50:00+05:30
---

# Forge: Slack + Archive + Tab Rebrand — Research Notes

## Research Summary

All research was conducted during the prior conversation turn using:

- **jcodemunch MCP** — code intelligence for blast radius and symbol analysis on the indexed Forge repo
- **zen:analyze MCP (gpt-5.4, high thinking mode)** — second-opinion architectural validation
- **Direct codebase reading** — confirmed every claim from jcodemunch with `Read` and `Bash grep`

Key takeaways:

1. The event bus (`server/internal/events/bus.go`) is a clean pub/sub seam — Slack hooks in cleanly
2. `IssueStatus` has 79-file blast radius — relabel-only beats schema change by orders of magnitude
3. The browser tab brand leak is in `apps/web/app/layout.tsx` and `apps/web/public/favicon.svg`
4. The integrations-tab.tsx already has a card pattern (GitHub) — Slack mirrors it

## Codebase Analysis

### Relevant Files Examined

| File | Purpose | Relevance |
|---|---|---|
| `server/internal/events/bus.go` | Event bus pub/sub | Slack listener will subscribe |
| `server/cmd/server/notification_listeners.go` | All notification logic | Hook point at line ~654 |
| `server/internal/handler/issue.go` | Issue CRUD + lifecycle | Publishes `EventIssueUpdated` with `statusChanged` payload |
| `server/migrations/001_init.up.sql` | DB schema | Status `CHECK` constraint hardcoded |
| `packages/core/types/issue.ts` | `IssueStatus` union type | 79-file blast radius |
| `packages/core/issues/config/status.ts` | Status labels + colors | Archive relabel target |
| `packages/views/locales/en/issues.json` | i18n labels | Archive relabel target |
| `packages/views/locales/en/projects.json` | i18n labels | Archive relabel target |
| `packages/views/settings/components/integrations-tab.tsx` | GitHub integration card | Pattern to mirror for Slack |
| `packages/views/settings/components/notifications-tab.tsx` | Notification preferences UI | Pattern reference for Switch components |
| `apps/web/app/layout.tsx` | Next.js root layout | Tab title + favicon metadata |
| `apps/web/public/favicon.svg` | SVG favicon | The Multica asterisk leak |
| `apps/web/app/(landing)/layout.tsx` | Landing page layout | Additional "Multica" string |

### Existing Patterns Identified

**Pattern 1: Event Bus Listeners** (`notification_listeners.go`)
- `bus.Subscribe(eventType, handler)` registers
- `registerNotificationListeners(bus, queries)` is the single registration point in `router.go`
- Each event type has a dedicated subscription function that pulls fields from `e.Payload.(map[string]any)`
- Used by: notification system, activity log, subscriber list updates
- **For Slack**: register one more subscription on `EventIssueUpdated` inside the existing function

**Pattern 2: Settings Tab Card** (`integrations-tab.tsx`)
- `<Card><CardContent>` wrapping
- Header row with icon + title/description + admin button
- Body with config inputs
- `canManage = currentMember?.role === "owner" || "admin"` gates editing
- Members see read-only hint
- Toast for save success/failure
- **For Slack**: add second `<Card>` directly below the GitHub one, same structure

**Pattern 3: TanStack Query for workspace-scoped data** (`notification-preferences/queries.ts`)
- `xxxOptions(wsId)` returns `{ queryKey: ["xxx", wsId], queryFn: ... }`
- `useUpdateXxx()` returns a mutation with `onSuccess: () => qc.invalidateQueries(...)`
- **For Slack**: `slackIntegrationOptions(wsId)` and `useUpdateSlackIntegration()`

**Pattern 4: Admin-only routes** (`router.go:447`)
- `r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))`
- Applied to entire route group, not per-handler
- **For Slack**: wrap all 4 Slack routes in one `r.Route()` with this middleware

**Pattern 5: Handler UUID parsing** (CLAUDE.md "Backend Handler UUID Parsing Convention")
- `parseUUIDOrBadRequest(w, s, fieldName)` for boundary inputs
- Loader functions for entity resolution
- **For Slack**: use `parseUUIDOrBadRequest(w, wsIdParam, "workspace_id")` in all 4 handlers

### Integration Points

| System | How |
|---|---|
| Event bus | `bus.Subscribe(protocol.EventIssueUpdated, ...)` — add ONE call in existing function |
| sqlc | New `server/pkg/db/queries/slack.sql`, regenerate with `make sqlc` |
| RBAC middleware | Reuse `middleware.RequireWorkspaceRole(queries, "owner", "admin")` |
| Frontend Settings | Add second `<Card>` to `integrations-tab.tsx` |
| i18n | Add keys to `packages/views/locales/en/settings.json` |
| TanStack Query | New `packages/core/slack-integration/{queries,mutations,index}.ts` |
| API client | New methods on `ApiClient` in `packages/core/api/client.ts` |

## jcodemunch Blast Radius Analysis

### `IssueStatus` (packages/core/types/issue.ts:3)

```
importer_count: 79
direct_dependents: 4
overall_risk_score: 0.5869
confirmed (10): types, inbox, queries, mutations, cache-helpers, draft-store,
                view-store, config/status.ts, ws-updaters, api.ts
potential (69): agents, auth, autopilots, chat, hooks, paths, permissions,
                pins, projects, realtime, runtimes, workspace, ...
impact_by_depth:
  depth 1: 4 files (risk 1.0)
  depth 2: 49 files (risk 0.62)
  depth 3: 25 files (risk 0.46)
```

**Implication**: Renaming any enum value in `IssueStatus` is a 79-file change. Adding a value is similar. Relabeling (UI string change) is 5 files.

### `BOARD_STATUSES` (packages/core/issues/config/status.ts:24)

```
importer_count: 11
direct_dependents: 1
overall_risk_score: 0.5399
confirmed (2): config/index.ts, issues/queries.ts
potential (9): cache-helpers, mutations, stores/*, ws-updaters, labels/mutations,
              realtime/use-realtime-sync
```

**Implication**: `BOARD_STATUSES` excludes `cancelled` already (line 24). The board doesn't show cancelled items today. This is why we say `cancelled` is already-archive-behaviorally.

### `STATUS_CONFIG` (packages/core/issues/config/status.ts:33)

```
importer_count: low (used in same module mostly)
overall_risk_score: low
```

**Implication**: Changing just the display strings in `STATUS_CONFIG` is contained. The `label` field is the user-visible text.

### Hardcoded status comparisons in `server/internal/handler/issue.go`

| Line | Code | Purpose |
|---|---|---|
| 338 | `whereClause += " AND i.status NOT IN ('done', 'cancelled')"` | Filter out closed |
| 384-390 | `CASE i.status WHEN 'in_progress' THEN 0 ... WHEN 'cancelled' THEN 6` | Sort order |
| 1118 | `status = "todo"` | Default on create |
| 1581 | `prevIssue.Status == "backlog" && issue.Status != "done" && issue.Status != "cancelled"` | Agent dispatch trigger |
| 1593 | `if statusChanged && issue.Status == "cancelled"` | Cancel running tasks |
| 1675 | `if issue.Status == "backlog"` | Skip dispatch |
| 1983, 1993 | Same patterns in batch update | Batch path |

**Implication**: If we ever rename `cancelled` to `archive` as a value, all 9 lines need change. Confirms ADR-005 (relabel-only).

### `statusLabels` (server/cmd/server/notification_listeners.go:24)

```go
var statusLabels = map[string]string{
    "backlog":     "Backlog",
    "todo":        "Todo",
    "in_progress": "In Progress",
    "in_review":   "In Review",
    "done":        "Done",
    "blocked":     "Blocked",
    "cancelled":   "Cancelled",  // ← change to "Archive"
}
```

Used in notification body strings ("moved to {statusLabel}"). One line change.

### `terminalStatusForTaskFailedDismiss` (server/cmd/server/notification_listeners.go:149)

```go
var terminalStatusForTaskFailedDismiss = map[string]bool{
    "in_review": true,
    "done":      true,
    "cancelled": true,  // ← cancelled is already terminal
}
```

**Implication**: `cancelled` is already treated as terminal/archive in the notification system. The relabel is purely cosmetic.

## zen:analyze Findings (gpt-5.4, high thinking, architecture)

Full output: see prior conversation turn at 2026-05-13 ~21:30.

### Strategic findings

1. **Statuses are a cross-cutting domain primitive, not a presentation-layer enum** — Adding new statuses requires re-modeling workflow semantics: actionable, terminal, archived, board-visible, dispatch-triggering, searchable. (Critical)

2. **Slack notifications fit the existing event-driven architecture with low technical debt** — Best hook is inside the `if statusChanged` block at `notification_listeners.go` lines 654–672. Workspace-scoped config, not per-user. (High)

3. **The real complexity cliff is "let each workspace define workflow semantics"** — Adding one fixed status is finite; per-workspace custom statuses is a workflow-engine project. (High)

4. **Upstream merge safety depends on isolating changes behind new seams** — Add new files/packages, minimize edits to upstream-hot files. (High)

5. **Notification architecture is strong, but Slack delivery should not inherit inbox preference semantics** — Slack target selection is integration routing, not recipient preference. (Medium)

6. **Hidden opportunity: extract status semantics to backend helpers** — `IsClosedStatus`, `IsTerminalStatus`, `ShouldDispatchOnTransition`, `IsBoardVisible`. Reduces blast radius for any future status change. (Medium)

### Direct answers to spec questions

| Question | zen answer |
|---|---|
| Minimal Slack architecture? | Listener in `notification_listeners.go` + new `slack/` package + workspace_slack_integrations table |
| Fixed `archive` vs custom statuses? | Cliff is at *custom semantics*. Recommend: relabel `cancelled` → "Archive" in UI only |
| Upstream sync risk? | Slack: LOW (isolated package). Statuses: HIGH if you widen the union type |
| Upgrade path when upstream adds these? | Slack: swap our `slack/` package for upstream's. Statuses: rename only is trivial; data migration if "archive" becomes a real value |

## Slack Incoming Webhooks — External Reference

### Endpoint

`POST https://hooks.slack.com/services/T.../B.../X...`

Headers: `Content-Type: application/json`

### Simple payload (v1)

```json
{
  "text": "📋 *[MUL-42] Redesign onboarding flow*\nStatus: *Todo* → *In Progress*\nAssigned to: @jane\n<https://forge.asymbl.app/asymbl/issues/MUL-42|View issue>"
}
```

### Block Kit payload (v2, deferred)

```json
{
  "blocks": [
    {
      "type": "header",
      "text": { "type": "plain_text", "text": "Issue updated: MUL-42" }
    },
    {
      "type": "section",
      "fields": [
        { "type": "mrkdwn", "text": "*Status*\nTodo → In Progress" },
        { "type": "mrkdwn", "text": "*Assignee*\n@jane" }
      ]
    },
    {
      "type": "actions",
      "elements": [
        { "type": "button", "text": { "type": "plain_text", "text": "View" }, "url": "https://forge.asymbl.app/asymbl/issues/MUL-42" }
      ]
    }
  ]
}
```

### Response

- 200 OK on success (body: `ok`)
- 4xx on bad payload or unknown channel
- 5xx on Slack outage
- Rate limit: 1/sec per webhook (we're well under that)

### Security notes

- Webhook URL is itself the secret (no separate API key)
- No signature verification on Slack's side — Forge trusts the URL is valid
- SSRF prevention: validate URL starts with `https://hooks.slack.com/services/`

## Recommended Approach Summary

1. **Slack notifications** — new package + 5-line hook. Low risk, high value.
2. **Archive relabel** — UI label change in 5 files. Zero risk.
3. **Tab rebrand** — 2 file edits. Zero risk.
4. **Defer**: Slack DMs (need bot token), multiple webhooks per workspace (table supports it), custom statuses (workflow engine), encryption (security platform concern).

## Anti-Patterns to Avoid

1. **Don't put Slack code in `issue.go`** — couples HTTP handler to integration side effect. Use the event bus.
2. **Don't reuse `NotificationPreferences` table for Slack** — blurs user-preference vs workspace-routing.
3. **Don't widen `IssueStatus` to `string`** — destroys 79 files of type safety.
4. **Don't add `archive` as a new DB value** — 9+ Go lines + DB migration + agent dispatch logic.
5. **Don't synchronously POST to Slack** — couples Forge latency to Slack health.
6. **Don't retry Slack POST in-process** — lost on restart anyway. Defer to queue infra.

## Dependency Analysis

### Recommended Dependencies

| Dependency | Version | Purpose | License |
|---|---|---|---|
| (none — Go stdlib `net/http`) | — | HTTP POST to Slack | BSD |
| (none — TS `fetch`) | — | API client | — |

### Dependency Risks

- None. Pure stdlib implementation.

## Open Questions from Research

- [ ] Should Slack messages use Forge's icon as the bot avatar? (Slack webhooks accept `icon_emoji` or `icon_url`) — defer to v2 polish
- [ ] Multi-locale messages? — defer (English only in v1)
- [ ] Should we expose a "test webhook" preview before save? — already in spec as P1

## Sources

- jcodemunch MCP: blast radius queries on `IssueStatus`, `BOARD_STATUSES`, `STATUS_CONFIG`, `canCreateAgent`
- zen:analyze MCP (gpt-5.4, high thinking): architectural review of both features
- Slack docs: https://api.slack.com/messaging/webhooks
- Slack Block Kit: https://api.slack.com/block-kit
- Slack rate limits: https://api.slack.com/docs/rate-limits
- Forge upstream sync PR #2: https://github.com/shivasymbl/forge/pull/2
- Direct codebase reads (jcodemunch + Read tool): notification_listeners.go, issue.go, status.ts, layout.tsx, favicon.svg
