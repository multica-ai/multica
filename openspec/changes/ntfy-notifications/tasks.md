# Tasks: ntfy Push Notifications + Notification Settings

## Backend

 user, ntfy_url TEXT, ntfy_token TEXT, disabled_types TEXT[] DEFAULT '{}', updated_at TIMESTAMPTZ, UNIQUE(user_id)). Create matching `.down.sql`.
- [ ] ** SQL queries + sqlc codegen**: Create `server/pkg/db/queries/notification_preference.sql` with `GetNotificationPreference` (by user_id) and `UpsertNotificationPreference` (ON CONFLICT upsert). Run `make sqlc`.T2 
1). Set `Authorization: Bearer <token>` and `X-Click` headers.
- [ ] ** Notification pipeline hook**: In `server/cmd/server/notification_listeners.go`, add `maybeSendNtfy(ctx, queries, sender, recipientUserID, inboxItem)` helper (look up pref, skip if ntfy_url empty or type in disabled_types, `go sender.Send(...)` non-blocking). Call it after every `notifyDirect` invocation. Wire the `Sender` in `main.go`.T4 
- [ ] ** Notification preference handler**: Create `server/internal/handler/notification_preference.go` with `GetNotificationPreference`, `UpsertNotificationPreference`, and `TestNotificationPreference` (sends test push using URL/token from request body). Register routes in `server/cmd/server/router.go`: `GET /notification-preferences`, `PUT /notification-preferences`, `POST /notification-preferences/test` (all JWT-protected).T5 
- [ ] ** Backend tests**: Unit test for ntfy sender (mock HTTP server). Integration tests for preference CRUD endpoints. Test that `maybeSendNtfy` skips correctly when ntfy_url is empty or type is in disabled_types.T6 

## Frontend

- [ ] ** Types and API client methods**: Add `NotificationPreference` type to `apps/workspace/src/shared/types/`. Add `getNotificationPreferences()`, `updateNotificationPreferences(pref)`, and `testNotificationPreference(url, token)` methods to the API client.T7 
- [ ] ** Notifications settings tab**: Create `apps/workspace/src/features/settings/components/notifications-tab.tsx` with a ntfy URL + token input section, a "Send Test Notification" button (uses current form values), and per-type toggles grouped into: Assignments, Status & Priority, Dates, Comments & Reactions, Agent Tasks. Save on form submit.T8 
- [ ] ** Wire into Settings page**: In `features/settings/components/settings-page.tsx`, add `{ value: "notifications", label: "Notifications", icon: Bell }` to `accountTabs` and add `<TabsContent value="notifications"><NotificationsTab /></TabsContent>`.T9 
- [ ] ** Frontend tests**: Unit tests for the notifications tab (renders, saves, test button, toggle behavior).T10 
