# ObitaPlus Frontend Design

## Scope

This document captures the implemented frontend design for the dedicated `obitaPlus` workspace chat experience.

## Navigation

- Workspace sidebar adds `obitaPlus` at `/{workspaceSlug}/obitaplus`.
- `obitaPlus` is placed above `项目`, occupying the previous `Issues` slot.
- `Issues` moves below `小队`.
- The sidebar footer no longer renders the Discord promo card or the help bubble entry.

## Page Structure

- `obitaPlus` is a route-backed workspace page, not a floating overlay.
- The page uses a full-height three-column workspace shape: the existing workspace sidebar remains on the left, the central chat region fills the available space, and a persistent chat history rail sits on the right on desktop widths.
- The central chat region constrains messages and the input to the same readable max width used by the floating chat surface, so user and assistant messages do not pin themselves to the page edges.
- The right history rail lists non-archived chat sessions, highlights the active session, and preserves the existing running, unread, rename, delete, and stop-run affordances from the chat history dropdown.
- The right history rail owns the primary `New chat` action on the dedicated page so the creation affordance is visible without relying on an icon-only tooltip. The central chat header hides its duplicate icon-only create action when the rail is present.
- Existing floating chat entry points (`ChatWindow`, `ChatFab`) are hidden when the current route is `/{workspaceSlug}/obitaplus`.

## Chat Reuse

- The dedicated page reuses `packages/views/chat/components/chat-window.tsx`.
- `ChatWindow` now supports two variants:
  - `floating`: existing resizable bottom-right overlay
  - `page`: full-height embedded panel without resize or minimize controls; the page owner may hide the header history trigger when a persistent history rail is present
- Both variants share the same session, agent selection, message timeline, banners, and input logic.
- `ChatSessionHistoryPanel` reuses the same chat-session query and history row behavior as the floating dropdown, but renders it as a desktop right rail for `obitaPlus`.
