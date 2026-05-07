# @multica/cerebro-chat

Cerebro additions to the chat surface that live as **net-new** files.
The upstream chat components themselves (chat-input.tsx, chat-window.tsx,
chat-message-list.tsx) stay in `@multica/views/chat/components/` with
inline `CEREBRO-PATCH` markers — they are heavily fork-modified upstream
code, not pure cerebro code.

Owns:

- `views/components/chat-status-line.tsx` — agent status banner with
  cancel/regenerate.
- `views/components/tool-summary.ts` — chat-tool name/icon resolution.
- `views/components/chat-input.test.tsx`,
  `views/components/chat-message-list.test.tsx`,
  `views/components/chat-window.test.tsx` — vitest specs that exercise
  the upstream chat components from cerebro's perspective (kept here so
  upstream-merges don't surface them as conflicting fork-additions).

## Why the Go chat tests stay in `server/internal/handler/`

`chat_attachment_test.go`, `chat_cancel_test.go`, `chat_coalesce_test.go`,
`chat_test.go` all rely on the package-level `testHandler`, `testPool`,
`setupHandlerTestFixture` that live in upstream's `handler_test.go`
(real DB connection + Hub + Bus + EmailService spin-up via `TestMain`).
Moving them out of `package handler` would force re-implementing that
fixture per cerebro test-package — significant duplication for files
that already sit in the upstream-zone-validator's test-exclude list.
Tracked for revisit if/when an upstream sync demands they relocate.
