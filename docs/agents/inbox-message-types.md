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
| `thread` | `InboxItem` (the thread's newest unread reply) | A **channel/DM thread** that has unread replies — surfaced as its own row so a reply buried inside a thread is not missed (FIR-1854). **Dynamic inbox only.** | **Channels** / **DM** (same view as its channel) |

> **`thread` is dynamic-inbox-only** (`packages/cerebro-inbox-dynamic`), gated by
> `cerebro_inbox_thread_split` (inbox group, default on). It is built in
> `useDynamicInboxData` by grouping channel/DM reply inbox items by
> `details.thread_root_id` (set server-side — see the `inbox-thread-split` patch
> in `docs/cerebro-patches.md`) and keeping only threads with an unread reply.
> The row reuses the `notif` row chrome (`InboxListItem`) and every shared row
> action, but opens its **channel** at the thread side-panel via the reply's
> `details.comment_id`. It carries `channelId` / `channelKind` / `threadRootId`
> so `matchesViewFilter` files it under Channels / DM (never Issues) and
> favorites/classification treat it like its channel. The channel's top-level
> messages still fold into the single `channel` row.

So the "4 types" a user sees in the inbox view filter — **Issues, Channels, DM,
Chat** — are really **3 row kinds**, where `channel` splits into Channels vs DM
by its `kind` field. The view filter lives in `matchesView()` in
`inbox-page.tsx`.

### Per-row actions and cross-type parity (TECH-3352)

Every row kind exposes the same action affordance so the inbox feels uniform:

- **Desktop:** a hover "**⋯**" (3-dot) dropdown menu.
- **Mobile:** a swipe surface — swipe-right to archive, swipe-left to reveal
  read/snooze, long-press for the full action drawer.

When `cerebro_inbox_rounds` is enabled, issue-notification rows add **Add to
Round** to the desktop `...` menu and **Round** to the mobile swipe-left panel
and long-press drawer. Channel, DM, chat, and thread rows do not show the action
because Round membership is issue-scoped. The picker and mutation are owned by
`@multica/cerebro-rounds`; `CerebroInboxRowActions` only opens that shared
picker so desktop, mobile, and issue detail use the same membership behavior.
The picker is a bottom drawer on mobile and a dialog on desktop.

The Rounds inbox surface is an optional `rounds` section in the dynamic Inbox
layout; it is never injected outside the user's saved section order. It uses the
same sortable, removable, collapsible block contract as other Inbox sections.
Collapsed Rounds shows no count. Expanded Rounds provides in-block search and
renders live members through the shared Inbox row renderer; missing/stale Inbox
rows are omitted instead of falling back to a second row design.

Round-member issues notify **only inside the Rounds box** (FIR-3114): their
inbox rows are excluded from the other dynamic-inbox sections, from every
unread count badge (sidebar/dock hook `useCerebroInboxUnreadCount`, the three
server count queries in `server/pkg/db/queries/inbox.sql`), and from
mobile/desktop push + in-app banner (`suppressPushForRoundIssue` in
`server/cmd/server/notification_listeners.go`). The inbox_item rows are still
created and keep their read state — inside the Rounds box a member row renders
unread only while its round has an active run; outside a run new responses
accumulate quietly until the next round surfaces them. During a run the member
list folds answered issues (held reply exists) behind an "Answered (n)"
collapse, the header counts `answered/total`, and a ready run can be paused
(`POST /api/cerebro/rounds/{roundId}/dismiss`) to collapse the round back to
its planned state. A batch round auto-starts when every agent response in the
ready run has received a reply.

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

### Reminder creation entrypoints (FIR-2385)

User-created reminders are stored as `cerebro_reminder` rows first. The due
sweeper later creates or re-surfaces the inbox row when `remind_at` fires. The
code map lives in `packages/cerebro-inbox/mutations.ts`; keep these entrypoints
on the unified endpoint/table:

| Surface | Code path | Backend |
|---|---|---|
| Inbox toolbar / free reminder | `useCreateGlobalReminder` | `POST /api/cerebro/reminders` |
| Legacy inbox reminder export | `useCreateInboxReminder` -> `api.createInboxReminder` | `POST /api/cerebro/reminders` |
| Inbox row, channel, DM snooze | `useSnoozeAsReminder` | `POST /api/cerebro/reminders` |
| Focus-list snooze | `packages/cerebro-focus-list/mutations.ts` `useSnoozeFocusItemAsReminder` | `POST /api/cerebro/reminders` |
| Issue/channel/DM comment menu | `useCreateCommentReminder` | `POST /api/cerebro/reminders` |
| Agent chat message menu | `packages/cerebro-chat/mutations.ts` `useCreateChatMessageReminder` | `POST /api/cerebro/reminders` |
| Reminders overview sheet | `packages/cerebro-reminders/core/mutations.ts` `useCreateReminder` | `POST /api/cerebro/reminders` |
| Mobile comment reminder | `apps/mobile/data/api.ts` `createCommentReminder` | `POST /api/cerebro/reminders` |
| Legacy clients | `server/internal/cerebro/inbox/handler.go` `CreateReminder` | accepts `/api/inbox/reminders`, writes `cerebro_reminder` |

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

### Archived block — dynamic inbox only (FIR-1645)

Archived messages have two surfaces in the dynamic inbox:

- The full-screen **Archived view** reached from the ⋯ → "Show archived" menu
  (`ArchivedInboxView`, TECH-3541 #3) — unchanged.
- A foldable **Archived box** added from "+ Add section"
  (`ArchivedInboxBlock`, `packages/cerebro-inbox-dynamic/components/archived-inbox-block.tsx`).
  It is **not** part of the merged inbox feed — like the Chat block it renders
  over its own archived queries via the shared `useArchivedInboxEntries` hook
  (archived inbox notifications + archived chats + archived channels/DMs,
  FIR-2791 via `GET /api/channels?archived_only=true`). An archived channel/DM
  shows as **one** row: its message notifications are folded into the channel
  row (`buildArchivedEntries` drops notifs whose `issue_id` is an archived
  channel). It **starts folded** by
  default and offers an in-block search, sort (newest/oldest), and
  group-by-type (Issues / Channels / Chat). It needs no extra flag — it lives
  inside the existing `cerebro_inbox_dynamic` inbox.

Unlike every other archived row (single "unarchive" via `CerebroUnarchiveAction`),
an inbox-message row **inside the Archived block** carries a **second** restore
action — "**unarchive & mark unread**". Rather than thread a prop through the
upstream `InboxListItem`, the block wraps each message row in
`ArchivedRowActionsProvider` (`packages/cerebro-inbox/archived-row-actions.tsx`)
and `CerebroUnarchiveAction` reads that context to upgrade its single restore
button into a two-action menu. Absent the provider (the classic archived view),
the button stays a plain single "unarchive". Chat rows keep their own restore
flow and get no "mark unread" affordance. Channel/DM rows (FIR-2791) restore
via `useUnarchiveChannel` (single "unarchive" only, both surfaces).

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
`agent_context_change_request` (FIR-1775 Agent Office — fires when someone
proposes a versioned edit to an agent's context; recipients are the context
owner + named approvers minus the proposer, `severity` `action_required`,
`route` `inbox` by default with push opt-in; `details` carries `agent_id`,
`agent_name`, `change_request_id`, `base_version`, `proposed_version`; carries no
`issue_id` — the inbox UI deep-links from `details.agent_id` to the agent's
Instructions tab. (Skill change-request rows, by contrast, no longer navigate
away: FIR-2742 opens them **in the inbox pane** via `SkillChangeInboxDetail`,
which lists the diff from `details` and offers an explicit "Open in new window".
Both classic and Dynamic Inbox use this message-pane behavior.)
Emitted by the
agent-office handler and routed by `registerCerebroAgentOfficeNotificationListener`),
`runtime_auto_paused`, `manually_added`, `agent_capability_drift` (TECH-3738
Bid C — the capability drift watcher alerts workspace owners/admins, `severity`
`attention`, when an agent uses a tool its declared policy denies; `details`
carries `agent_id`, `agent_name`, `drift_tools`, `drift_count`; system-authored,
`route` `inbox`. Emitted by the `driftwatch` sweeper, gated by the
`cerebro_capability_drift_watcher` flag, default OFF).

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

**Standalone-row lifecycle (FIR-2278).** A *fired* `reminder` row (the cerebro
reminder sweeper drops one for every reminder type — free, project, issue-,
comment-, chat-anchored) is treated as a **standalone signal**, never folded
into its issue's group:

- **Dedup** (`deduplicateInboxItems`, `packages/core/inbox/queries.ts`): a fired
  (due, not future-muted) reminder is keyed by its own id, so it neither hides
  nor is hidden by other rows on the same issue.
- **Badge** (`CountUnreadInboxForUserAllWorkspaces`): groups reminders by id too,
  so the OS badge matches the visible inbox.
- **Archive** (`ArchiveInboxItem`): archiving a reminder row archives only that
  row, not the issue's other notifications.
- **Cleanup**: `done` / `snooze` / `delete` archive the fired inbox row via
  `cerebro_reminder.fired_inbox_item_id` (reminder handler), so a reminder never
  lingers in the inbox after the user acts on it.

---

## Mentions come from three sources (issue comment, note, note comment)

`mentioned` is a single `InboxItemType`, but it is produced from **three** places —
all reuse the same comment-mention engine and the per-user
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
- **Note comment mention** (FIR-2589) — `@`-tagging a member in a **comment on a
  note**. `CreateComment` (`server/internal/cerebro/note/comments.go`) calls
  `notifyCommentMentions`, which reuses the exact note-body path above: it
  shares the note with the tagged member and publishes the same
  `EventNoteMentioned`. Unlike a body mention it also carries the comment, so
  the event payload adds `comment_id` (the thread-root id) and `comment_excerpt`.
  The listener puts `comment_id` in `details` and the excerpt in the item **body**,
  so the inbox message reads with context and the deep-link opens the exact
  comment: the inbox appends `&comment=<id>` to `?note=<id>` and the Notes surface
  (`NotesPage`/`NoteEditor` `initialCommentId`) opens the comments panel and
  scrolls to that comment. Still no new event or notification type — a comment
  mention is a `mentioned` item with `details.comment_id` also set.

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
