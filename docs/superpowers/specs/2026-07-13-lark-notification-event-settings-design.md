# Workspace Feishu Notification Event Settings Design

## Goal

Allow workspace administrators to choose which Multica inbox event types are delivered through the workspace Feishu Notification Bot. Keep actionable events enabled by default and make other currently-produced events optional.

## Scope

The setting is workspace-scoped because each workspace has one Notification Bot installation. Owners and admins can edit it. Other workspace members can view the effective selection but cannot change it.

The setting controls only Feishu delivery. It does not change whether Multica creates an inbox item. A notification is delivered to Feishu only when both conditions hold:

1. The workspace Notification Bot enables the event type.
2. The recipient's existing Multica notification preference allows the underlying inbox notification to be created.

## Event Catalog

The UI exposes only event types with a current production path.

| Category | Event type | Default |
| --- | --- | --- |
| Assignment | `issue_assigned` | Enabled |
| Assignment | `unassigned` | Disabled |
| Assignment | `assignee_changed` | Disabled |
| Comments and mentions | `mentioned` | Enabled |
| Comments and mentions | `new_comment` | Disabled |
| Comments and mentions | `reaction_added` | Disabled |
| Issue updates | `status_changed` | Disabled |
| Issue updates | `priority_changed` | Disabled |
| Issue updates | `start_date_changed` | Disabled |
| Issue updates | `due_date_changed` | Disabled |
| Agent execution | `task_failed` | Enabled |
| Quick create | `quick_create_failed` | Enabled |
| Quick create | `quick_create_done` | Disabled |
| Automation | `autopilot_paused` | Enabled |
| Automation | `issue_subscribed` | Disabled |

`task_completed`, `agent_blocked`, and `agent_completed` are not displayed because they do not currently create inbox items.

## Persistence

Add `notification_event_types TEXT[]` to `lark_installation`. It is meaningful only when `installation_kind = 'notification'`.

The database default contains:

```text
issue_assigned
mentioned
task_failed
quick_create_failed
autopilot_paused
```

Notification Bot reinstall updates the existing workspace installation row and preserves the selected event types. Agent/runtime Bot rows do not use this field.

The server owns the event catalog and validates updates against the supported list. Unknown values are rejected instead of being silently stored.

## API

The existing installation list response includes `notification_event_types` for the Notification Bot.

Add an admin-only workspace endpoint to update the Notification Bot event selection. The request contains the complete enabled event list, making replacement semantics explicit and avoiding partial-toggle races.

The endpoint returns the updated installation representation. An empty list is valid and disables all Feishu notifications without disconnecting the Bot.

## Delivery

The Feishu inbox notifier reads the selected events from the active workspace Notification Bot installation. It no longer uses a hard-coded five-event allowlist.

Issue-linked events keep the current message-to-issue mapping. Replying to such a Feishu message creates an issue comment and runs the standard comment-trigger rules.

Events without an `issue_id`, currently `quick_create_failed` and `autopilot_paused`, can still be delivered as direct messages. Their message omits the Issue field and the instruction about replying to comment. No `lark_inbox_notification` issue-reply mapping is created for them.

Delivery remains best-effort. A Feishu failure does not roll back the Multica inbox item.

## UI

Place a "Notification events" section directly below the workspace Notification Bot connection status in Workspace Settings -> Feishu.

Render events as grouped binary switches, not nested cards. Show the effective state to all members and disable editing for non-admins. Admin changes use an explicit Save action so several switches can be reviewed and submitted atomically.

The UI uses the server response as the source of truth and resets unsaved changes when refreshed. Saving invalidates the workspace Lark installation query.

## Error Handling

- Updating without an active workspace Notification Bot returns a not-found response.
- Non-admin updates return forbidden.
- Unknown event types return bad request.
- Failed saves preserve local selections and show an error toast.
- Missing or malformed response fields fall back to the documented default event list in installed clients.

## Testing

Backend tests cover default values, validation, admin authorization, replacement updates, delivery filtering, issue-less message formatting, and preservation of reply mapping for issue-linked events.

Core API tests cover schema parsing and malformed-response fallback. Shared view tests cover grouping, default selection, admin editing, read-only member state, save success, and save failure.

