# Workspace Feishu Notification Event Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let workspace administrators select which currently-produced Multica inbox events are delivered by the workspace Feishu Notification Bot.

**Architecture:** Store the enabled event types on the workspace Notification Bot installation, expose replacement-style admin API semantics, and make the inbox notifier consult the persisted selection. Keep the event catalog centralized in the Go Lark package, add defensive TypeScript parsing, and render grouped switches in a focused shared settings component.

**Tech Stack:** Go 1.26, Chi, PostgreSQL/sqlc, TypeScript, Zod, TanStack Query, React, Vitest, Testing Library.

---

### Task 1: Persist Notification Bot Event Selection

**Files:**
- Create: `server/migrations/127_lark_notification_events.up.sql`
- Create: `server/migrations/127_lark_notification_events.down.sql`
- Modify: `server/pkg/db/queries/lark.sql`
- Regenerate: `server/pkg/db/generated/lark.sql.go`
- Regenerate: `server/pkg/db/generated/models.go`

- [ ] **Step 1: Add the forward migration**

```sql
ALTER TABLE lark_installation
    ADD COLUMN notification_event_types TEXT[] NOT NULL DEFAULT ARRAY[
        'issue_assigned',
        'mentioned',
        'task_failed',
        'quick_create_failed',
        'autopilot_paused'
    ]::TEXT[];
```

- [ ] **Step 2: Add the rollback migration**

```sql
ALTER TABLE lark_installation
    DROP COLUMN notification_event_types;
```

- [ ] **Step 3: Preserve settings during Notification Bot reinstall**

Keep `notification_event_types` out of the `UpsertLarkNotificationInstallation` conflict update so the existing row retains administrator choices.

- [ ] **Step 4: Add replacement and lookup queries**

```sql
-- name: UpdateLarkNotificationEventTypes :one
UPDATE lark_installation
SET notification_event_types = sqlc.arg('notification_event_types'),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND installation_kind = 'notification'
  AND status = 'active'
RETURNING *;
```

Restrict `GetActiveLarkBindingForWorkspaceUser` to `i.installation_kind = 'notification'` so workspace notification policy is never sourced from an Agent Bot row.

- [ ] **Step 5: Regenerate sqlc output**

Run: `make sqlc`

Expected: generated `LarkInstallation` includes `NotificationEventTypes []string`, and `UpdateLarkNotificationEventTypes` is available on `*db.Queries`.

- [ ] **Step 6: Run migration/sqlc compilation checks**

Run: `cd server && go test ./pkg/db/generated -run '^$'`

Expected: PASS.

- [ ] **Step 7: Commit the persistence slice**

```bash
git add server/migrations/127_lark_notification_events.* server/pkg/db/queries/lark.sql server/pkg/db/generated/lark.sql.go server/pkg/db/generated/models.go
git commit -m "feat(lark): persist notification event selection"
```

### Task 2: Define the Event Catalog and Admin API

**Files:**
- Create: `server/internal/integrations/lark/notification_events.go`
- Create: `server/internal/integrations/lark/notification_events_test.go`
- Modify: `server/internal/handler/lark.go`
- Modify: `server/internal/handler/lark_test.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write failing catalog tests**

Test the exact supported and default event lists, duplicate normalization, stable catalog ordering, and rejection of an unknown value such as `made_up_event`.

```go
func TestValidateNotificationEventTypes(t *testing.T) {
    got, err := ValidateNotificationEventTypes([]string{"mentioned", "issue_assigned", "mentioned"})
    require.NoError(t, err)
    require.Equal(t, []string{"issue_assigned", "mentioned"}, got)

    _, err = ValidateNotificationEventTypes([]string{"made_up_event"})
    require.Error(t, err)
}
```

- [ ] **Step 2: Run the catalog test and confirm RED**

Run: `cd server && go test ./internal/integrations/lark -run TestValidateNotificationEventTypes -count=1`

Expected: FAIL because the catalog and validator do not exist.

- [ ] **Step 3: Implement the server-owned catalog**

Define these currently-produced events in stable UI order:

```go
var SupportedNotificationEventTypes = []string{
    "issue_assigned", "unassigned", "assignee_changed",
    "mentioned", "new_comment", "reaction_added",
    "status_changed", "priority_changed", "start_date_changed", "due_date_changed",
    "task_failed",
    "quick_create_failed", "quick_create_done",
    "autopilot_paused", "issue_subscribed",
}

var DefaultNotificationEventTypes = []string{
    "issue_assigned", "mentioned", "task_failed", "quick_create_failed", "autopilot_paused",
}
```

`ValidateNotificationEventTypes` must deduplicate and return values in catalog order. Empty input is valid.

- [ ] **Step 4: Write failing handler tests**

Cover:

- owner/admin can replace the complete list;
- member receives `403`;
- unknown events receive `400`;
- no active Notification Bot receives `404`;
- empty list succeeds;
- installation list response includes `notification_event_types`.

- [ ] **Step 5: Run handler tests and confirm RED**

Run: `cd server && go test ./internal/handler -run 'Test(UpdateLarkNotificationEvents|ListLarkInstallations)' -count=1`

Expected: FAIL because the route/handler/response field does not exist.

- [ ] **Step 6: Implement replacement API semantics**

Add request and response wiring:

```go
type UpdateLarkNotificationEventsRequest struct {
    EventTypes []string `json:"event_types"`
}

func (h *Handler) UpdateLarkNotificationEvents(w http.ResponseWriter, r *http.Request) {
    // Decode, validate through lark.ValidateNotificationEventTypes,
    // update by workspace ID, map pgx.ErrNoRows to 404, return installation response.
}
```

Add this admin-only route beside install/revoke:

```go
r.Put("/lark/notification-events", h.UpdateLarkNotificationEvents)
```

Add `notification_event_types` to `LarkInstallationResponse` and `larkInstallationToResponse`.

- [ ] **Step 7: Run catalog and handler tests**

Run: `cd server && go test ./internal/integrations/lark ./internal/handler -run 'Test(ValidateNotificationEventTypes|UpdateLarkNotificationEvents|ListLarkInstallations)' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit the API slice**

```bash
git add server/internal/integrations/lark/notification_events.go server/internal/integrations/lark/notification_events_test.go server/internal/handler/lark.go server/internal/handler/lark_test.go server/cmd/server/router.go
git commit -m "feat(lark): add workspace notification event API"
```

### Task 3: Apply Selection During Feishu Delivery

**Files:**
- Modify: `server/internal/integrations/lark/inbox_notifier.go`
- Modify: `server/internal/integrations/lark/inbox_notifier_test.go`

- [ ] **Step 1: Write failing delivery tests**

Cover these behaviors with real notifier helpers and fake query/client dependencies:

```go
func TestShouldDeliverInboxNotificationUsesInstallationSelection(t *testing.T) {
    item := inboxNotificationItem{recipientType: "member", typ: "status_changed"}
    if !shouldDeliverInboxNotification(item, []string{"status_changed"}) {
        t.Fatal("enabled optional event must be delivered")
    }
    if shouldDeliverInboxNotification(item, DefaultNotificationEventTypes) {
        t.Fatal("disabled optional event must not be delivered")
    }
}
```

Also assert:

- `quick_create_failed` without `issue_id` sends a DM;
- issue-less text omits `Issue：` and the reply-to-comment instruction;
- issue-less delivery does not call `CreateLarkInboxNotification`;
- issue-linked delivery still persists the reply mapping;
- member and configured-client gates remain intact.

- [ ] **Step 2: Run notifier tests and confirm RED**

Run: `cd server && go test ./internal/integrations/lark -run 'Test(ShouldDeliverInboxNotificationUsesInstallationSelection|InboxNotifierIssueLess|FormatInboxNotificationText)' -count=1`

Expected: FAIL because filtering is hard-coded and issue-less events are rejected.

- [ ] **Step 3: Replace the hard-coded allowlist**

Load the active Notification Bot installation before evaluating its selection. Change the helper contract to:

```go
func shouldDeliverInboxNotification(item inboxNotificationItem, enabled []string) bool
```

It must require a member recipient and membership in `enabled`, but must not require a valid issue ID.

- [ ] **Step 4: Split issue-linked and issue-less formatting**

For issue-linked events, preserve:

```text
Multica 收件箱通知
Issue：MUL-123
标题：...
类型：...
级别：...

回复此飞书消息，即可在对应 issue 下发表评论。
```

For issue-less events, emit title/type/severity/body only. Persist `lark_inbox_notification` only when `item.issueID.Valid`.

- [ ] **Step 5: Add Chinese labels for newly deliverable types**

Extend `inboxNotificationTypeLabel` for every supported event so administrators never enable an event that produces a raw English enum in Feishu.

- [ ] **Step 6: Run the full Lark package**

Run: `cd server && go test ./internal/integrations/lark -count=1`

Expected: PASS.

- [ ] **Step 7: Commit the delivery slice**

```bash
git add server/internal/integrations/lark/inbox_notifier.go server/internal/integrations/lark/inbox_notifier_test.go
git commit -m "feat(lark): filter delivery by workspace event settings"
```

### Task 4: Add Defensive Core API Support

**Files:**
- Modify: `packages/core/types/lark.ts`
- Create: `packages/core/lark/schema.ts`
- Create: `packages/core/lark/schema.test.ts`
- Create: `packages/core/lark/mutations.ts`
- Modify: `packages/core/lark/index.ts`
- Modify: `packages/core/api/client.ts`

- [ ] **Step 1: Write failing schema tests**

Test that a valid installation list preserves event types and malformed/missing fields fall back to the documented defaults.

```ts
expect(parseLarkInstallation({
  id: "inst-1",
  installation_kind: "notification",
  notification_event_types: ["mentioned", "task_failed"],
}).notification_event_types).toEqual(["mentioned", "task_failed"]);
```

- [ ] **Step 2: Run schema tests and confirm RED**

Run: `pnpm --filter @multica/core test -- lark/schema.test.ts`

Expected: FAIL because the schema/parser does not exist.

- [ ] **Step 3: Define shared types and defaults**

```ts
export type LarkNotificationEventType =
  | "issue_assigned" | "unassigned" | "assignee_changed"
  | "mentioned" | "new_comment" | "reaction_added"
  | "status_changed" | "priority_changed" | "start_date_changed" | "due_date_changed"
  | "task_failed"
  | "quick_create_failed" | "quick_create_done"
  | "autopilot_paused" | "issue_subscribed";

export const DEFAULT_LARK_NOTIFICATION_EVENTS: LarkNotificationEventType[] = [
  "issue_assigned", "mentioned", "task_failed", "quick_create_failed", "autopilot_paused",
];
```

Add optional `notification_event_types?: LarkNotificationEventType[]` to `LarkInstallation` for desktop compatibility.

- [ ] **Step 4: Parse list and update responses**

Use Zod plus `parseWithFallback` in `listLarkInstallations` and the update method. Do not cast network JSON.

```ts
async updateLarkNotificationEvents(
  workspaceId: string,
  eventTypes: LarkNotificationEventType[],
): Promise<LarkInstallation> {
  const raw = await this.fetch<unknown>(`/api/workspaces/${workspaceId}/lark/notification-events`, {
    method: "PUT",
    body: JSON.stringify({ event_types: eventTypes }),
  });
  return parseWithFallback(raw, LarkInstallationSchema, EMPTY_LARK_INSTALLATION, {
    endpoint: "PUT /api/workspaces/:id/lark/notification-events",
  });
}
```

- [ ] **Step 5: Add a TanStack mutation factory**

`packages/core/lark/mutations.ts` must call the API and invalidate `larkKeys.installations(wsId)` on success.

- [ ] **Step 6: Run core tests and typecheck**

Run: `pnpm --filter @multica/core test -- lark/schema.test.ts`

Run: `pnpm --filter @multica/core typecheck`

Expected: PASS.

- [ ] **Step 7: Commit the core slice**

```bash
git add packages/core/types/lark.ts packages/core/lark/schema.ts packages/core/lark/schema.test.ts packages/core/lark/mutations.ts packages/core/lark/index.ts packages/core/api/client.ts
git commit -m "feat(core): expose lark notification event settings"
```

### Task 5: Build the Workspace Settings UI

**Files:**
- Create: `packages/views/settings/components/lark-notification-events.tsx`
- Create: `packages/views/settings/components/lark-notification-events.test.tsx`
- Modify: `packages/views/settings/components/lark-tab.tsx`
- Modify: `packages/views/settings/components/lark-tab.test.tsx`
- Modify: `packages/views/locales/en/settings.json`
- Modify: `packages/views/locales/zh-Hans/settings.json`
- Modify: `packages/views/locales/ja/settings.json`
- Modify: `packages/views/locales/ko/settings.json`

- [ ] **Step 1: Read translation conventions before editing copy**

Read:

- `apps/docs/content/docs/developers/conventions.mdx`
- `apps/docs/content/docs/developers/conventions.zh.mdx`

- [ ] **Step 2: Write failing component tests**

Cover:

- five defaults are on when the server field is missing;
- persisted values override defaults;
- all 15 events render in six categories;
- owner/admin can toggle and save the complete list;
- member switches and save action are disabled/hidden;
- save success invalidates/refetches installation data;
- save failure keeps the draft and shows an error toast.

- [ ] **Step 3: Run component tests and confirm RED**

Run: `pnpm --filter @multica/views test -- settings/components/lark-notification-events.test.tsx`

Expected: FAIL because the component does not exist.

- [ ] **Step 4: Implement the focused event settings component**

Use `Switch` for each event and an explicit Save button. Render grouped, unframed sections rather than nested cards. Accept these props:

```ts
interface LarkNotificationEventsProps {
  workspaceId: string;
  installation: LarkInstallation;
  canManage: boolean;
}
```

Initialize draft state from `installation.notification_event_types ?? DEFAULT_LARK_NOTIFICATION_EVENTS`. Save the full selected list through the core mutation.

- [ ] **Step 5: Integrate below Notification Bot status**

Render the component only when an active Notification Bot exists. Keep the connection card and install dialog behavior unchanged.

- [ ] **Step 6: Add localized event/category copy**

Add keys under `notifications.lark.events` for title, description, save states, six category labels, 15 event labels/descriptions, read-only hint, and success/failure toasts. Follow the mixed-language issue/task terminology in the conventions.

- [ ] **Step 7: Run view tests and typecheck**

Run: `pnpm --filter @multica/views test -- settings/components/lark-notification-events.test.tsx settings/components/lark-tab.test.tsx`

Run: `pnpm --filter @multica/views typecheck`

Expected: PASS.

- [ ] **Step 8: Commit the UI slice**

```bash
git add packages/views/settings/components/lark-notification-events.tsx packages/views/settings/components/lark-notification-events.test.tsx packages/views/settings/components/lark-tab.tsx packages/views/settings/components/lark-tab.test.tsx packages/views/locales/*/settings.json
git commit -m "feat(settings): configure feishu notification events"
```

### Task 6: Verify and Restart the Local App

**Files:**
- Verify all files changed in Tasks 1-5.

- [ ] **Step 1: Run focused backend verification**

Run: `cd server && go test ./internal/integrations/lark ./internal/handler ./cmd/server -count=1`

Expected: PASS.

- [ ] **Step 2: Run focused frontend verification**

Run: `pnpm --filter @multica/core test -- lark/schema.test.ts`

Run: `pnpm --filter @multica/views test -- settings/components/lark-notification-events.test.tsx settings/components/lark-tab.test.tsx`

Run: `pnpm typecheck`

Expected: PASS.

- [ ] **Step 3: Apply migrations and restart the backend**

Use the checkout's `.env`, run `cd server && go run ./cmd/migrate up`, stop only the process listening on the configured backend port, and restart `go run ./cmd/server`. Do not change any CLI profile.

- [ ] **Step 4: Verify runtime state**

Confirm:

- `GET http://localhost:8080/health` returns `{"status":"ok"}`;
- startup logs contain `lark integration enabled`;
- frontend remains available on `http://localhost:3000`;
- installation API returns the five default event types for the existing Notification Bot;
- saving an optional event persists and survives a fresh installation-list fetch.

- [ ] **Step 5: Perform browser UI verification**

Open Workspace Settings -> Feishu at desktop and mobile widths. Confirm grouped switches do not overlap, long localized labels wrap, non-admin state is read-only, and Save state does not shift layout.

