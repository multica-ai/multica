# Agent documentation

This folder holds **authoritative, agent-facing reference docs** for working on
this codebase. Unlike `docs/plans/` or the `*-plan.md` files (which capture a
moment in time), everything here is meant to stay true to the live code — if you
change behavior these docs describe, update the doc in the same PR.

If you are an AI agent working on Firtal Cerebro / Multica, read the doc that
matches what you are about to touch before you start.

## Index

- [`permission-system.md`](./permission-system.md) — **What an agent is allowed
  and denied at runtime today, and by which mechanism.** Read this before
  touching anything that grants, denies, gates, or approves an agent action
  (tool access, credentials, repo checkout, web fetch, sandbox, mentions,
  group/autopilot scope, the tool-policy chain). It separates what is enforced
  **live today** from what is **off by default**, because confusing the two has
  already caused a wrong conclusion once.

- [`system-activity/`](./system-activity/README.md) — **System Activity — the
  platform wakeup mechanism.** How `schedule_wakeup` / `list_wakeups` /
  `cancel_wakeup` work, what constraints apply (15-min min interval,
  consecutive-postpone limit), and how the sweeper dispatches due wakeups. Read
  before touching `server/internal/cerebro/wakeup/` or any MCP wakeup tools.

- [`inbox-message-types.md`](./inbox-message-types.md) — **What shows up in the
  inbox.** The 3 row kinds (issue notifications, channels/DM, agent chat) plus
  the richer `InboxItemType` notification taxonomy underneath (mentions,
  reminders, agent activity, …), and how per-row actions stay identical across
  kinds (desktop 3-dot menu + shared `MobileRowActions` swipe). Read before
  touching the inbox list, a `Cerebro*RowActions` component, or adding a
  notification type. Covers the reminder family and how note @-mentions surface
  as `mentioned` inbox items.
