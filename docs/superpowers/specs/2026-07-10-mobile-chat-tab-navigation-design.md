# Mobile Chat Tab: List-First Navigation — Design

Date: 2026-07-10
Status: Approved

## Context

`apps/mobile`'s Chat tab (`app/(app)/[workspace]/(tabs)/chat.tsx`) is currently
a single-screen IA: tapping the tab auto-hydrates to the most recently
active chat session and renders that conversation directly (a mobile-only
simplification — web/desktop's `SessionDropdown` in `chat-window.tsx` is
also a dropdown, not a full session-list page; neither client has an
"all chats" browse page today). Switching sessions, starting a new chat,
and deleting a session all happen through local state
(`activeSessionId`/`selectedAgentId`) plus a formSheet route
(`app/(app)/[workspace]/chat-sessions.tsx`) that hands its selection back
through a small cross-screen store, `data/stores/chat-session-picker-store.ts`
(`activeSessionId` mirror + one-shot `selectRequest`).

There is no `chat/[id]` sub-route — this file's own doc comment says so
explicitly, and it's a deliberate prior decision, not an oversight.

## Goal

Flip the Chat tab's landing behavior: tapping the tab lands on a list of
all chat sessions (what `chat-sessions.tsx`'s sheet shows today, as a real
page instead of a sheet); tapping a session pushes into that session's
conversation screen. Everything else — send/optimistic-update/draft/
auto-markRead/realtime/new-chat/delete behavior — carries over unchanged,
just relocated to whichever of the two screens now owns it.

This is a mobile-only UX divergence from web/desktop's dropdown-based
session switcher — per `apps/mobile/CLAUDE.md`'s "Behavioral parity" rule,
this is acceptable because it changes UI/interaction, not product
semantics (same sessions, same unread state, same deletion capability).

## Non-goals

- No content enhancements to the list rows (e.g., last-message preview).
  `ChatSession` has no last-message field; adding one would mean N+1
  fetches or a backend change, out of scope for a navigation-structure
  change. Rows show exactly what `chat-sessions.tsx`'s sheet shows today:
  agent avatar (with presence dot), title, unread dot, archived label.
- No changes to send/optimistic-update/draft/auto-markRead/realtime
  business logic — only where each piece is mounted changes.
- No changes to the tab-bar unread badge (`useChatUnreadSessionCount`,
  wired in `(tabs)/_layout.tsx`) — it's already independent of which
  screen is showing.
- No changes to `AgentPickerSheet`, `ChatMessageList`, `ChatComposer`,
  `NoAgentBanner`, `OfflineBanner` — reused as-is by whichever screen
  needs them.

## Changes

### 1. `(tabs)/chat.tsx` becomes the session-list page

Replaces the current single-screen conversation view. Renders what
`chat-sessions.tsx` renders today (avatar/title/unread-dot/archived-label
rows, tap → push to conversation, long-press → delete-confirm same as
today), inside mobile's own `<Header>` component (matching the tab-root
pattern already used by Inbox/My Issues/Skills/Runtimes — headerShown is
false at the `Tabs` level, so every tab root renders its own `<Header>`).
Header title: `common.tabs.chat` ("Chat"), matching every sibling tab
root's convention of using its own tab name, not `chat.sessions.title`
("Chats") which the sheet used. Header right: a single "+" `IconButton`
(reuses `ChatSessionActions`'s existing new-chat button and its
`handleNewChat` logic: >1 available agent → open `AgentPickerSheet`; else
→ skip straight to a blank compose for the sole agent).

### 2. New pushed route `chat/[id].tsx` — the conversation screen

Everything from today's `chat.tsx` that isn't list-rendering: message
list, composer, send/optimistic-update burst, draft read/write keyed by
session id, auto-markRead-on-focus, `useChatSessionRealtime(id)`,
delete-current-session (kept as a header "⋯" action, same
`handleDeleteActive` behavior — confirms, deletes, then `router.back()`s
to the list since there's no more "blank same-tab" state to fall back to).
`activeSessionId` becomes the route's `id` param instead of local state.
Registered in `[workspace]/_layout.tsx` like `issue/[id]`/`skill/[id]`/
`runtime/[id]` — a native Stack header, not the custom `<Header>`
component (this file is a pushed detail screen, not a tab root).

Header title reuses `ChatTitleButton`'s existing avatar+name+subtitle
rendering (via `Stack.Screen`'s `headerTitle`), but drops the `onPress`
affordance — there's no sheet left to open; the native back button
already returns to the list. `ChatTitleButton`'s `onPress` prop becomes
optional; passing none renders a non-interactive header title.

### 3. New pushed route `chat/new.tsx` — the not-yet-created-session case

Handles today's "new chat, no session id yet" state (reached from the
list's "+" button or `AgentPickerSheet`'s pick), parameterized by
`?agentId=`. Shares the same rendering as `chat/[id].tsx` (message list
empty state, composer, draft keyed by the `DRAFT_NEW_SESSION` sentinel
that already exists) but has no `id` to mount realtime/markRead against.
On first send, `ensureSession` creates the real session (unchanged
lazy-creation behavior) and the screen `router.replace()`s to
`chat/[sessionId]` so the back stack and any deep link land on the real
route, not the transient `chat/new`.

Registered in `_layout.tsx` alongside `chat/[id]`, same native-Stack-header
treatment (header title shows the picked agent's name via the same
`ChatTitleButton` render, non-interactive, same as `chat/[id]`).

### 4. Removed

- `app/(app)/[workspace]/chat-sessions.tsx` (the formSheet) — fully
  replaced by the new `(tabs)/chat.tsx` list page.
- `data/stores/chat-session-picker-store.ts` and its
  `useChatSessionPickerResetOnWorkspaceChange` wiring in `_layout.tsx` —
  the cross-screen bridge existed only because session-picking happened
  on a separate sheet route reaching back into tab-local state; real
  navigation (route params) replaces that need entirely.
- The `chat-sessions` `SHEET_OPTIONS` `Stack.Screen` registration in
  `_layout.tsx`.

Per root `CLAUDE.md`'s "if a flow or API is being replaced and the
product is not live, prefer removing the old path instead of preserving
both" — this feature isn't live yet in the sense of having external
consumers of the sheet route, so straight removal instead of keeping a
dead code path.

## Testing

No new test files — matches the precedent for prior mobile screen work
in this project (typecheck + lint + manual pass).

## Verification

1. `pnpm --filter @multica/mobile typecheck`
2. `pnpm --filter @multica/mobile lint`
3. `pnpm --filter @multica/mobile test` (locale parity — no new namespace,
   but confirms nothing broke)
4. Manual: tap Chat tab → lands on the session list (not a conversation).
   Tap a session → conversation screen renders with correct history,
   composer works, send/receive still functions, auto-markRead still
   clears the unread dot, back button returns to the list with the dot
   gone. Tap "+" with multiple agents → agent picker → pick one → blank
   compose screen renders (`chat/new`) → send first message → screen
   silently becomes the real session (back button from here returns to
   the list, and the just-created session is visible with its title).
   Tap "+" with exactly one agent → skips the picker, same blank-compose
   flow. Long-press a session row on the list → delete confirm → row
   disappears. Open a session, delete it via header "⋯" → confirm → lands
   back on the list. Confirm the tab-bar unread badge still updates
   correctly throughout. Confirm both languages render correctly (no new
   strings needed, but confirm existing `chat.json` keys reused in the
   new locations still read correctly in 简体中文).
