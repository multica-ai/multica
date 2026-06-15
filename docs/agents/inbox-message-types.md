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

### Favorite star — dynamic inbox only (TECH-3579)

The dynamic inbox adds one affordance that is **not** part of the shared
row-action menu above: a **favorite toggle** overlaid on the leading avatar of
every row. It is intentionally an overlay (in `DynamicInboxRow`,
`packages/cerebro-inbox-dynamic/components/dynamic-inbox-row.tsx`), not a
`MobileRowActions` / dropdown item, because the product requirement is a
direct, in-place toggle on the avatar. It therefore does not touch the upstream
row components (`InboxListItem` / `ChannelListItem`) — the overlay is absolutely
positioned over the size-7 avatar all three kinds render at `px-4`.

The interaction is **two-step with a flip** (per Jesper, TECH-3579): the avatar
shows by default; clicking it rotates the overlay (`rotateY` flip) to reveal a
star; clicking the star sets the favorite. A favorited row shows the gold star
at rest; clicking it removes the favorite and flips back to the avatar. Clicking
the avatar only flips — it never opens/selects the row. This is `FavoriteStar`'s
local `armed` state; it works the same on touch (tap to flip, tap to favorite),
so there is no hover dependency.

- State lives in `useInboxFavorites` (`packages/cerebro-inbox-dynamic/use-favorites.ts`),
  persisted per user in the `cerebro_inbox_favorites` preferences key (no DB
  table) — the same blob the inbox layout uses, so favorites follow the user
  across devices. A conversation's key is `issue:<id>` / `chat:<id>` /
  `channel:<id>`.
- Starred rows float into a "Favorites" sub-section at the top of the
  **"All messages"** box (toggleable per box via `showFavoritesSection`), and
  feed a standalone **Favorites** section kind. Both read the same
  `isFavorite` predicate on `SectionFilterContext`.
- Gated by the `cerebro_inbox_favorites` flag (default on). The classic inbox
  has no favorite affordance.

---

## Axis 2 — Notification types (what happened, on a `notif` row)

A `notif` row carries a `type` field — the real taxonomy of "what happened on an
issue". The full union is `InboxItemType` in `packages/core/types/inbox.ts`
(every cerebro addition is marked with a `CEREBRO-PATCH(...)` comment there):

**Issue lifecycle:** `issue_assigned`, `issue_started`, `unassigned`,
`assignee_changed`, `status_changed`, `priority_changed`, `start_date_changed`,
`due_date_changed`.

**Conversation:** `new_comment`, `mentioned` (from a comment **or** a note — see
"Mentions come from two sources" below), `reaction_added`, `review_requested`.

**Agent activity:** `task_completed`, `task_failed`, `agent_blocked`,
`agent_completed`, `quick_create_done`, `quick_create_failed`,
`agent_comment_no_tag`, `agent_comment_member_tag`, `agent_comment_agent_tag`
(agent comments split by the tag they carry so monologues can be muted without
losing hand-offs).

**Reminders (a distinct family — see the dedicated section below):** `reminder`,
`due_date_reminder`, `start_date_reminder`.

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

## Reminders (the reminder family)

Three `InboxItemType`s form the **reminders** family — they share the
`reminders` action-category bucket (`REMINDER_TYPES` in
`packages/cerebro-inbox/action-groups.ts`, ~line 34):

- **`reminder`** — a **user-created** "remind me later". When you snooze any
  inbox row (the "Remind me" / "Påmind mig" action on issue, channel, DM and
  chat rows), the row is hidden until its time via `muted_until`
  (`packages/core/types/inbox.ts`), then resurfaces — it does not spawn a new
  row, it *is* the snoozed row coming back. The reminder picker is the shared
  `ReminderSheet` from `@multica/cerebro-inbox`.
- **`due_date_reminder`** / **`start_date_reminder`** — **system-generated** when
  an issue's due/start date arrives. Fired by the server-side sweepers
  `server/cmd/server/reminder_due_sweeper.go` and
  `server/cmd/server/issue_date_reminder_sweeper.go`.

So "a reminder" is genuinely its own message type (three of them), distinct from
the issue-lifecycle and conversation types above.

---

## Mentions come from two sources (comment and note)

`mentioned` is a single `InboxItemType`, but it is produced from **two** places —
both reuse the same comment-mention engine and the per-user
`"mentioned"` → `"comments"` notification setting:

- **Comment mention** — `@`-tagging a member in an issue comment. The inbox item
  is tied to the issue (`inbox_item.issue_id`) and deep-links to the issue.
- **Note mention** (Notes feature, TECH-3421) — `@`-tagging a member in a note.
  Implemented: saving a note publishes `EventNoteMentioned`
  (`server/internal/cerebro/note/handler.go`), and the listener
  `server/cmd/server/cerebro_note_mentions.go` (registered in
  `event_listener_wiring.go`) creates a `mentioned` item with severity `info`.
  Because a note is an **artifact, not an issue**, it cannot use
  `inbox_item.issue_id`; the note reference rides in the item's **`details` JSON**
  (`details.note_id`, `details.note_title`) and the inbox UI deep-links from
  `details.note_id`. The listener also shares the note with the mentioned member
  so the notification is openable.

> Takeaway for agents: a note mention is **not** a separate type — it is a
> `mentioned` notification whose `details.note_id` is set (issue-less). If you
> add another mention source, follow the same pattern: reuse `mentioned`, carry
> the target in `details`, don't invent a parallel type.

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
