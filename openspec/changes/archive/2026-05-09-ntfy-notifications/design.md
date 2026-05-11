# Design: ntfy Push Notifications + Notification Settings

## Problem

The current notification system delivers all notifications exclusively inside the app (inbox + WebSocket realtime). Users who close the browser tab receive no signal that something needs their attention. There is no mechanism to reach users through external push channels.

## Goals

- Allow each user to opt in to push notifications via ntfy.
- Let users control which notification types reach them via ntfy.
- Keep the inbox (in-app) behavior completely unchanged.
- Require zero server-side secrets for ntfy — the topic URL lives in user preferences.
- Be extensible: the `notification_preference` table is designed so future channels (e.g., email digests) can be added as columns.

## Non-Goals

- Workspace-level ntfy configuration.
- Email notification for inbox events (email is only used for auth today).
- Webhooks or other push channels.
- Batching / digesting notifications.

---

## Data Model

### New Table: `notification_preference`

```sql
CREATE TABLE notification_preference (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    ntfy_url       TEXT,            -- full topic URL, e.g. https://ntfy.sh/my-topic
    ntfy_token     TEXT,            -- optional Bearer token for private topics
    disabled_types TEXT[] NOT NULL DEFAULT '{}',  -- ntfy-suppressed types
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id)
);
```

**Notes:**
- `ntfy_url = NULL` means ntfy is disabled for this user (default).
- `disabled_types` stores a list of `InboxItemType` values. An empty array means all types are forwarded to ntfy.
- One row per user, created on first save (upsert).

---

## Backend Architecture

### ntfy Sender (`internal/ntfy/sender.go`)

A thin package with a single exported function:

```go
type Sender struct {
    httpClient *http.Client
}

type Message struct {
    Title    string
    Body     string
    Priority int    // 1-5, mapped from severity
    ClickURL string // deep-link back to the issue
}

func (s *Sender) Send(ctx context.Context, topicURL, token string, msg Message) error
```

- Uses `http.DefaultClient` with a 5-second timeout.
- Sets `Authorization: Bearer <token>` header when token is non-empty.
- Maps severity → ntfy priority:
  - `action_required` → 5 (urgent)
  - `attention` → 3 (default)
  - `info` → 1 (min)
- Non-blocking from caller's perspective — called via goroutine in notification pipeline.

### Notification Pipeline Extension

In `notification_listeners.go`, the existing `notifyDirect` / `notifySubscribers` functions already create an `inbox_item` and publish `inbox:new`. After the inbox item is created, a new helper `maybeSendNtfy` is called:

```
maybeSendNtfy(ctx, queries, ntfySender, recipientUserID, inboxItem)
  → queries.GetNotificationPreference(ctx, userID)
  → if ntfy_url == "" → return
  → if item.Type in disabled_types → return
  → go sender.Send(ctx, ntfy_url, ntfy_token, Message{...})
```

The `go` call makes the ntfy push non-blocking so it cannot slow down or break the existing notification path.

### SQL Queries (`pkg/db/queries/notification_preference.sql`)

```sql
-- name: GetNotificationPreference :one
SELECT * FROM notification_preference WHERE user_id = $1;

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preference (user_id, ntfy_url, ntfy_token, disabled_types, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (user_id) DO UPDATE SET
    ntfy_url       = EXCLUDED.ntfy_url,
    ntfy_token     = EXCLUDED.ntfy_token,
    disabled_types = EXCLUDED.disabled_types,
    updated_at     = NOW()
RETURNING *;
```

### Handler (`internal/handler/notification_preference.go`)

```
GET  /notification-preferences  → returns current user's preference (404 → default empty pref)
PUT  /notification-preferences  → upsert { ntfy_url, ntfy_token, disabled_types }
POST /notification-preferences/test → sends a test ntfy message using URL/token from request body
                                       (does NOT require prior save — enables "test before save" UX)
```

Auth: standard JWT middleware — `X-User-ID` header identifies the user.

---

## Frontend Architecture

### Settings Integration

The Settings page (`features/settings/components/settings-page.tsx`) already exists and uses a Tabs layout with "My Account" and workspace sections. Add a **Notifications** tab to the `accountTabs` array:

```typescript
{ value: "notifications", label: "Notifications", icon: Bell }
```

Then add the corresponding `<TabsContent value="notifications">` with the `NotificationSettingsTab` component.

**Components:**
```
NotificationSettingsPage
├── NtfyConfigSection
│   ├── URL input (ntfy topic URL)
│   ├── Token input (password type, optional)
│   └── "Send Test Notification" button
└── NotificationTypeToggles
    ├── Section: Assignments
    │   ├── issue_assigned
    │   ├── unassigned
    │   └── assignee_changed
    ├── Section: Status & Priority
    │   ├── status_changed
    │   ├── priority_changed
    │   └── review_requested
    ├── Section: Dates
    │   ├── due_date_changed
    │   ├── start_date_changed
    │   └── end_date_changed
    ├── Section: Comments & Reactions
    │   ├── new_comment
    │   ├── mentioned
    │   └── reaction_added
    └── Section: Agent Tasks
        ├── task_completed
        ├── task_failed
        ├── agent_blocked
        └── agent_completed
```

**State:** Local form state (React Hook Form or simple useState). Saves on blur / explicit save button.

**API calls:**
- `GET /notification-preferences` on mount.
- `PUT /notification-preferences` on save.
- `POST /notification-preferences/test` on test button click.

### New Types (`shared/types/notification-preference.ts`)

```typescript
export interface NotificationPreference {
  ntfy_url: string;
  ntfy_token: string;
  disabled_types: InboxItemType[];
}
```

---

## Notification Type Toggle UX

- All types are **ON by default** (empty `disabled_types`).
- Toggling a type OFF adds it to `disabled_types`.
- Disabling all types effectively silences ntfy without clearing the URL.
- The page shows a summary banner: "ntfy push notifications are active" / "Configure ntfy to receive push notifications outside the app."

---

## Deep Links

The `click` field in ntfy payloads will be:
```
<MULTICA_APP_URL>/issues/<issue_id>
```

`MULTICA_APP_URL` is already in the server config (used elsewhere).

---

## Error Handling

- ntfy send failures are logged at `WARN` level but never surface to the user or affect inbox creation.
- If `GetNotificationPreference` returns `not found`, ntfy is silently skipped.
- Invalid ntfy URLs produce an HTTP error that is logged and discarded.

---

## Migration

```
server/migrations/034_notification_preference.up.sql
server/migrations/034_notification_preference.down.sql
```

(Adjust number to next available.)
