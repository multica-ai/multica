# Telegram integration implementation notes

Status: complete for the text-message scope described below.

This document records the architecture and operating contract of Multica's
Telegram channel. Setup instructions for operators live in the localized
Telegram Bot integration guides under `apps/docs/content/docs/`.

## Product contract

| Area | Behavior |
| --- | --- |
| Transport | Telegram Bot API `getUpdates` long polling; no public callback URL is required |
| Installation | One Telegram bot token belongs to one active Multica workspace and agent |
| Identity | Every Telegram sender completes a one-time private account binding |
| Private chat | Text and slash commands are accepted directly |
| Group chat | A message is accepted when it mentions or replies to the bot; replying to a human additionally requires an explicit bot mention and includes that quoted sender/text |
| Forum topic | Session and reply routing retain the Telegram topic ID |
| Conversation | `/new` and `/issue` follow the shared channel command contract; accepted messages persist while unaddressed group chatter is not collected |
| Replies | The triggering Telegram message is quoted; task output is streamed by editing the first reply |
| Media | Text-only in this release; addressed non-text input receives an explicit unsupported-message response and creates no task |
| Token storage | Bot tokens are encrypted with `MULTICA_TELEGRAM_SECRET_KEY` and never returned by management APIs |

## Architecture

The implementation uses the shared channel framework rather than a parallel
Telegram-specific execution path:

- `channel.Channel`, `channel.Registry`, and `engine.Supervisor` own lifecycle,
  retries, routing, and task dispatch.
- `server/internal/integrations/telegram/` owns Bot API transport, installation,
  binding, inbound parsing, outbound delivery, Markdown conversion, and reply
  streaming.
- The generic channel installation, binding, session, and identity tables are
  reused with `channel_type = 'telegram'`.
- Migrations 349 and 350 extend the issue-origin constraint with
  `telegram_chat` using the repository's two-phase `NOT VALID` / `VALIDATE`
  pattern.
- `packages/core/` provides the typed management client and React Query keys;
  `packages/views/` provides shared settings, agent integration, and binding
  views; `apps/web/` provides the bind route.

## Safety boundaries

- Installation verifies the token with `getMe` before persistence. Transport
  failures and credential rejection remain distinct so a network outage does
  not encourage unnecessary credential rotation.
- Long polling refuses bots with an active outgoing webhook and never deletes
  another system's webhook automatically.
- Group binding links are never posted publicly. A group sender is directed to
  start a private bot chat to complete binding.
- Human-message quote context is included only when a group member replies and
  explicitly mentions the bot. The quoted sender and text/caption are persisted
  with the accepted instruction; `/new` starts a fresh session without dropping
  that selected quote. Ambient group chatter is never collected.
- Chat-level edit cooldown and `retry_after` state survive sequential tasks in
  a hard-bounded cache. When capacity pressure compresses an active chat entry,
  its deadline is merged into a conservative per-installation fallback so a
  recreated chat never calls Telegram before the mandatory wait expires.
- `EventChatDone` only enqueues in-process terminal delivery, so Telegram HTTP
  latency and repeated `retry_after` waits cannot block the synchronous event
  bus or realtime clients. A keyed scheduler preserves per-session FIFO order;
  normally one chat's backoff consumes no worker. If cache pressure compresses
  exact backoff state, other chats under the same Bot installation may be
  conservatively delayed. The terminal queue has fixed count, per-session, and
  byte limits; overflow is rejected and logged without blocking the event bus.
  Per-reply chunk progress prevents successful chunks from being repeated after
  a 429. Non-429 errors remain non-retriable.
- On shutdown, queued, retry-waiting, and in-flight terminal jobs are abandoned
  after request cancellation, and their local stream/schedule references are
  released. The fixed-capacity in-process queue is not restart-durable.
- Retry-pending task failures clear the superseded stream without showing a
  terminal failure notice; only a terminal failure changes or sends that notice.
- Outbound delivery requires authoritative Telegram task provenance and fails
  closed when task or origin lookup cannot be completed.
- Telegram API errors are sanitized so bot tokens cannot enter logs or API
  responses.

## Verification

The implementation is covered by Go protocol, lifecycle, security, migration,
and race tests; TypeScript API-schema, query, settings, binding-page, locale,
and UI tests; production Web/Docs/Desktop builds; and a real private-chat
Telegram regression covering binding, commands, task execution, issue origin,
streamed quoted replies, reconnect behavior, UTF-16 content, long replies, and
the text-only media response.

The detailed evidence and channel capability comparison are maintained in
`docs/telegram-channel-review-2026-08-08.md`.

## Deployment prerequisites

- Set a 32-byte base64 `MULTICA_TELEGRAM_SECRET_KEY` in the server environment.
- Ensure the server can reach `https://api.telegram.org/`; the standard Go
  transport honors `HTTPS_PROXY` when a deployment requires one.
- Apply repository migrations before enabling the integration.
- Keep Telegram privacy mode enabled unless a deployment deliberately wants
  the bot to receive every group message.

No production deployment, production configuration change, or real-user data
operation is part of this contribution.
