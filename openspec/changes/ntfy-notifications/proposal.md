## Why

The current notification system only delivers notifications inside the app via an inbox and WebSocket. Users miss important updates when they are away from the browser. Introducing ntfy support lets each user receive push notifications on their phone or desktop via the lightweight, self-hostable ntfy service — without requiring a third-party account or any server-side secrets.

## What Changes

- Add a `notification_preference` table that stores each user's ntfy topic URL, an optional auth token (for private topics), and a list of notification types they wish to exclude from ntfy.
- Extend the server-side notification pipeline so that whenever an `inbox_item` is created for a member, the system looks up that user's preferences and fires a non-blocking HTTP POST to their configured ntfy URL.
- Map inbox severity to ntfy priority levels: `action_required` → 5 (urgent), `attention` → 3 (default), `info` → 1 (min).
- Include a deep-link `click` URL in ntfy payloads so users can tap the notification and land directly on the relevant issue.
- Add two REST endpoints (`GET /notification-preferences`, `PUT /notification-preferences`) for reading and saving preferences.
- Add a **Settings → Notifications** tab in the workspace frontend where users can enter their ntfy URL and token, send a test notification, and toggle individual notification types on/off.

## Capabilities

### New Capabilities

- `ntfy-push`: Each user can opt in to receiving push notifications via their own ntfy topic. Preferences are global across all workspaces.
- `notification-settings`: A dedicated settings page that lets users configure their ntfy channel and control which notification types reach them.

### Modified Capabilities

- `inbox-notifications`: After inbox item creation, the server optionally forwards the notification to ntfy. Inbox behavior is unchanged.

## Impact

- New database migration: `notification_preference` table.
- New SQL queries and sqlc-generated Go code.
- New ntfy sender utility in the Go backend.
- Notification pipeline extended in `notification_listeners.go`.
- New REST endpoints and handler.
- New frontend page: `Settings > Notifications` with ntfy URL input, token input, test button, and per-type toggle list.
- No schema changes to existing tables.
- No changes to existing inbox or WebSocket behavior.
