# Inbox message types

Authoritative, agent-facing map of **what kinds of things show up in the inbox**,
how they are rendered, and how their per-row actions (3-dot menu on desktop,
swipe on mobile) are kept identical across types. Read this before touching the
inbox list, a row-action component, or anything that creates an inbox
notification.

> **Two different "type" axes.** People casually say "the inbox has 4 types".
> That is the **row-kind / grouping** axis (what the row *is*). Underneath, a
> single kind — the notification row — carries a much richer **notification-type**
> axis (what *happened*). Both are documented below; don't conflate them.

---

## Axis 1 — Row kinds (how a row is rendered)

The inbox merges three structurally different sources into one list. The union
is defined in `packages/views/inbox/components/inbox-page.tsx` (the `MergedEntry`
type, ~line 822):

| Row kind | Backing data | What it represents | User-facing grouping(s) |
|---|---|---|---|
| `notif` | `InboxItem` (`packages/core/types/inbox.ts`) | Everything about an **issue**: assignment, comments, mentions, reminders, agent activity, skill change requests, … | **Issues** (and Mentioned / Reminders sub-views) |
| `channel` | `Channel` (`kind: "channel" \| "dm"`) | A **channel** (multi-party) or a **DM** (1:1 between people) | **Channels** and **DM** |
| `chat` | `ChatSession` | A **1:1 agent chat** (a conversation with an AI agent) | **Chat** |

So the "4 types" a user sees in the inbox view filter — **Issues, Channels, DM,
Chat** — are really **3 row kinds**, where `channel` splits into Channels vs DM
by its `kind` field. The view filter lives in `matchesView()` in
`inbox-page.tsx`.

### Per-row actions and cross-type parity (TECH-3352)

Every row kind exposes the same action affordance so the inbox feels uniform:

- **Desktop:** a hover "**⋯**" (3-dot) dropdown menu.
- **Mobile:** a swipe surface — swipe-right to archive, swipe-left to reveal
  read/snooze, long-press for the full action drawer.

The mobile surface is **one shared component**, `MobileRowActions`, exported
from `@multica/cerebro-inbox`, reused by every row kind so mobile behaviour is
identical. The row-action component per kind:

| Row kind | Component | Desktop | Mobile | Feature flag |
|---|---|---|---|---|
| `notif` | `CerebroInboxRowActions` (`packages/cerebro-inbox/components/cerebro-inbox-row-actions.tsx`) | `DesktopRowActions` dropdown | `MobileRowActions` | `cerebro_inbox_row_actions` |
| `channel` | `CerebroChannelRowActions` (`packages/cerebro-channels/channel-row-actions.tsx`) | dropdown | `MobileRowActions` | `cerebro_channel_row_actions` |
| `chat` | `CerebroChatSessionRowActions` (`packages/cerebro-chat/views/components/chat-row-actions.tsx`) | dropdown | `MobileRowActions` | `cerebro_chat_row_actions` |

All three flags default **on** (`packages/cerebro-feature-flags/registry.ts`).
Chat has a few extra menu items the others don't (Rename, Convert to issue,
Delete) because a chat session is also a first-class object; the shared
read / snooze ("Remind me") / archive actions are the same everywhere.

> If you add a new row kind or a new per-row action, wire it through
> `MobileRowActions` + a hover dropdown so it stays identical across kinds —
> don't reimplement a one-off mobile gesture.

---

## Axis 2 — Notification types (what happened, on a `notif` row)

A `notif` row carries a `type` field — the real taxonomy of "what happened on an
issue". The full union is `InboxItemType` in `packages/core/types/inbox.ts`
(every cerebro addition is marked with a `CEREBRO-PATCH(...)` comment there):

**Issue lifecycle:** `issue_assigned`, `issue_started`, `unassigned`,
`assignee_changed`, `status_changed`, `priority_changed`, `start_date_changed`,
`due_date_changed`.

**Conversation:** `new_comment`, `mentioned`, `reaction_added`,
`review_requested`.

**Agent activity:** `task_completed`, `task_failed`, `agent_blocked`,
`agent_completed`, `quick_create_done`, `quick_create_failed`,
`agent_comment_no_tag`, `agent_comment_member_tag`, `agent_comment_agent_tag`
(agent comments split by the tag they carry so monologues can be muted without
losing hand-offs).

**Reminders (a distinct family):** `reminder` (a user-created "remind me later"
on any item), `due_date_reminder` and `start_date_reminder` (fired by the
server-side sweepers `reminder_due_sweeper.go` / `issue_date_reminder_sweeper.go`
when a date arrives).

**Platform / governance:** `private_agent_run_request`,
`skill_change_request_created`, `skill_change_request_reviewed`,
`runtime_auto_paused`, `manually_added`.

Each type has an `InboxSeverity` (`action_required` | `attention` | `info`) and
the set actually emitted by the server today is the `EmittedNotificationType`
subset in `packages/cerebro-notifications/core/routing.ts`. The backend source of
truth for emission + routing is `server/cmd/server/notification_listeners.go` and
`server/cmd/server/notification_routing.go` (both cerebro-only).

### Routing vs grouping — don't confuse these with the type

- **Routing key** (`packages/cerebro-notifications/core/routing.ts`) decides
  *where* a notification goes (inbox / notifications tab / off; some types split
  `.assignee` vs `.follower`). It is keyed off the type but is not the type.
- **Action category** (`InboxActionCategory` in
  `packages/cerebro-inbox/action-groups.ts`: `act_now` | `reminders` |
  `watching` | `pending` | `waiting` | `calm`) is how the inbox *groups* rows for
  the user. Also derived from the type, also not the type.

---

## Known gaps / not-yet types

- **Note mentions.** The Notes feature (TECH-3421, `packages/cerebro-notes` +
  `server/internal/cerebro/note/handler.go`) does CRUD + sharing but **emits no
  inbox notification today** — there is no `note_mention` `InboxItemType` and no
  listener for note events. If we want "you were mentioned in a note" to land in
  the inbox, it needs a new `InboxItemType`, a server listener, a routing key,
  and (optionally) an action-category mapping. This is the natural next message
  type to add.

---

## Where to change what

| You want to… | Touch |
|---|---|
| Add a new notification **type** | `packages/core/types/inbox.ts` (union + severity), `server/cmd/server/notification_listeners.go` (emit), `notification_routing.go` (route), `packages/cerebro-notifications/core/routing.ts` (TS routing + emitted set) |
| Add/adjust a per-row **action** | the matching `Cerebro*RowActions` component + `MobileRowActions` (`packages/cerebro-inbox`) — keep all kinds identical |
| Add a new **row kind** | `MergedEntry` + `matchesView` in `packages/views/inbox/components/inbox-page.tsx`, plus a `Cerebro*RowActions` component reusing `MobileRowActions` |
| Change how rows are **grouped** | `packages/cerebro-inbox/action-groups.ts` |

Keep this doc true to the code: if you add a type, a row kind, or change the
shared row-action contract, update this file in the same PR.
