# Telegram channel review and validation

Final validation: 2026-08-18

Baseline: `multica-ai/multica` `upstream/main` at `c670e0549`

Contribution branch: `codex/telegram-integration`

## Outcome

Telegram is implemented as a first-class Multica channel for text-message
workflows. The contribution includes backend transport and lifecycle, secure
installation and account binding, private/group/forum-topic routing, streamed
quoted replies, slash commands, issue provenance, settings and agent UI,
localized setup documentation, landing copy, migrations, and regression tests.

The review found and corrected integration, security, transport, UI, and data
provenance gaps before this contribution was prepared. External review was
used as advisory input; all acceptance decisions were made from the current
source and local verification evidence.

## Completed corrections

- Reject long-polling installation when Telegram has an outgoing webhook;
  Multica does not silently delete another integration's webhook.
- Sanitize Bot API transport errors so a bot token cannot enter logs or client
  responses.
- Require authoritative task and channel provenance before sending Telegram
  output, with fail-closed behavior on lookup errors.
- Keep bearer binding links out of groups and validate the binding token's
  channel type during redemption.
- Distinguish invalid credentials from network/proxy failure and bound
  credential verification to 15 seconds.
- Use Telegram's current `reply_parameters` request shape and retain the
  triggering message ID through streamed delivery.
- Parse mention and command entities with Telegram UTF-16 offsets and exact bot
  username matching.
- Key streamed output by immutable task ID so simultaneous tasks in one chat
  cannot share accumulated output.
- Preserve explicitly quoted human-message text/caption and sender attribution
  in group context while keeping the sender's own instruction isolated in
  `CommandText`; `/new` is stripped before enrichment so the selected quote
  remains attached to the fresh instruction, and unaddressed group chatter
  remains uncollected.
- Share stream throttling and Telegram `retry_after` state per chat in a
  hard-bounded schedule cache. Capacity compression merges active deadlines
  into a conservative per-installation fallback before deleting exact state.
- Enqueue `EventChatDone` into a fixed-capacity in-process keyed scheduler that
  preserves per-session FIFO order. Fixed workers execute one request step,
  while edit throttles and arbitrary/continuous 429 waits return to a retry
  heap without occupying a worker. If cache pressure compresses exact backoff
  state, other chats under the same Bot installation may be conservatively
  delayed. Global count, per-session count, and total-byte limits reject and
  log overflow without blocking the event bus. Saved chunk progress avoids
  repeating successful sends; ambiguous non-429 transport failures remain
  non-retriable to avoid duplicates.
- Shutdown cancels in-flight requests, abandons queued and retry-waiting jobs,
  and releases their local stream/schedule references; restart durability
  remains outside the in-process scheduler's contract.
- Release task stream state on cancellation and retry-pending failures without
  emitting a false terminal failure notice; terminal failures still notify.
- Enforce Telegram's UTF-16 message size limit, restrict plain-text fallback to
  entity-parsing errors, and prevent duplicate delivery after ambiguous
  transport failures.
- Provide `/new` and `/issue`, including `@botname` command suffixes, usage
  replies, duplicate-issue handling, and explicit failure responses.
- Preserve forum-topic IDs for typing indicators, unsupported-media notices,
  session routing, and outbound replies.
- Fail closed on malformed management responses instead of rendering false
  install or account-binding success.
- Add Telegram to agent integration visibility, settings, realtime cache
  invalidation, all four product locales, setup documentation, and landing
  release copy.
- Extend issue-origin validation with `telegram_chat` through migrations 349
  and 350 and verify both migration directions and catalog state.

## Capability comparison

| Channel | Text | Thread/topic | Quote | Typing | Edit/stream | Attachment | Rich card |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Lark | yes | yes | yes | yes | yes | yes | yes |
| Slack | yes | yes | no | no | no | no | no |
| DingTalk | yes | no | no | no | no | yes | no |
| WeCom | yes | no | no | no | no | yes | no |
| Telegram | yes | yes | yes | yes | yes | text-only | no |

Telegram's media behavior is an explicit product boundary, not an accidental
omission. Addressed non-text input receives a documented response and creates
no task. Rich cards are Lark-specific and are not a cross-channel requirement.

## Automated verification

The following change-scoped checks passed on the contribution:

- `pnpm typecheck`
- `pnpm lint` with zero errors (repository warnings unchanged)
- `pnpm build` for Web, Docs, and Desktop
- `go test ./internal/migrations -count=1`
- `go test -race ./internal/integrations/telegram -count=1`
- `go test ./internal/handler -count=1`
- focused Telegram settings, binding-page, API-schema, locale-parity, migration,
  install-management, routing, UTF-16, quote, reconnect, and streaming tests
- `git diff --check`
- credential-pattern and archive-path scans over the contribution source

The repository-wide TypeScript suite was also exercised in serial mode. It
completed 364 Views files / 4,325 tests with one unrelated Autopilot dialog
test timing out; that file then passed 4/4 in an isolated rerun. Focused final
parity runs covered agent/Telegram settings, locale parity, Telegram binding,
and core API schemas.

The repository-wide race suite was exercised through `make test`. Every
Telegram and migration test passed. Five unrelated timing/host-process cases
in repocache, GitHub snapshot refresh, and channel-media reconciliation failed
in the concurrent run; all five passed with `-race` in isolated reruns. No
repository Playwright spec targets Telegram, so the deterministic Bot API
protocol suite is the relevant E2E boundary for these review corrections.

## Real Telegram verification

Using a local Multica server, migrated local database, local runtime, and an
existing test bot, the following behaviors were observed against the live
Telegram Bot API:

- Bot installation and one-time private account binding.
- Private inbound message -> Multica task -> runtime execution -> streamed
  outbound Telegram reply.
- `/new` and `/issue`, including bare commands and
  `@multica_channel_bot` suffix forms. The incomplete `/fresh` alias was removed
  in favor of the shared, persisted `/new` contract.
- Reply quoting through `reply_parameters` for command verdicts and normal
  agent output.
- A 20-line response, reconnect after server restart, rapid sequential input,
  and a supplementary-plane Emoji response (`TG_UTF16_😀_OK`).
- A real `/issue TG_ORIGIN_MIGRATION_REAL_20260814` created issue `SYCO-1`; its
  persisted row has `origin_type = 'telegram_chat'` and a non-null `origin_id`.
- A real 26-byte text-file upload received the documented unsupported-media
  response and created no task.
- The Desktop agent integration page displayed Telegram with open and
  disconnect actions; “Open in Telegram” launched the configured bot chat.

Private-chat behavior is covered by both live E2E and automated tests.
Group mention/reply, quoted human-message context, command isolation, and
forum-topic behavior are covered by deterministic Bot API protocol tests. The
live account's group chooser contained no isolated test group, so the regression
deliberately avoided modifying unrelated groups or participants.

## Security and release boundaries

- No bot token, binding URL, Telegram sender/chat ID, API key, private key,
  cookie, browser state, environment file, or database content is stored in
  this contribution or its source archive.
- The first release is intentionally text-only; unsupported media follows an
  explicit, tested response path.
- Telegram availability requires deployment reachability to
  `api.telegram.org`; the adapter uses the standard proxy-aware Go transport.
- A bot token has one active workspace owner, preventing competing long-poll
  consumers.
- Migrations were applied only to the authorized local test database. No
  production deployment, production configuration, or real-user data action
  was performed.

## Review provenance

External advisory review:
https://chatgpt.com/c/6a773603-2798-83ec-a368-e298da9da653

The external response was not treated as acceptance evidence. Codex reviewed
the implementation, applied corrections, and independently ran the checks and
real Telegram regression recorded above.
