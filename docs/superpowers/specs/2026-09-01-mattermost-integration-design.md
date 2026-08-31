# Mattermost chat integration — design

Date: 2026-09-01
Issue: OBER-2
Status: approved

## Goal

Connect a Multica agent to a Mattermost bot account, so workspace members can
DM the bot, `@`-mention it in a channel, run `/issue`, `/new` and `/clear`, and
get the agent's reply back in the originating conversation or thread.

Mattermost is the sixth IM channel. The platform-agnostic framework in
`server/internal/integrations/channel` already owns everything that is not
Mattermost-specific, so this is an adapter, not new plumbing.

## What already exists

- `channel` — the `Channel` / `InboundMessage` / `OutboundMessage` /
  `Capability` / `Registry` contract. Adding a platform means registering a
  factory, never editing the core.
- `channel/engine` — `Supervisor` (one supervised connection per installation,
  Redis-fenced lease, reconnect backoff), `Router` (dedup, identity binding,
  session persistence, shared `/issue` `/new` `/clear` command parsing),
  `ChatSession`, `OutboundReplier` and `TypingNotifier` seams.
- Generic `channel_installation`, `channel_user_binding`,
  `channel_binding_token`, `channel_chat_session_binding`,
  `channel_task_delivery`, `channel_inbound_dedup` tables. No new table.
- Five adapters to model on. `telegram` and `slack` are the closest: both are
  bring-your-own-bot, both are the smallest, both are the newest.

## Decisions

These were confirmed on OBER-2 before implementation started.

### 1. Transport: WebSocket

Mattermost exposes `GET /api/v4/websocket`. The client dials it, sends an
`authentication_challenge` action carrying the bot access token, and then reads
a stream of events. That is a persistent per-installation connection — the same
shape `engine.Supervisor` already drives for Feishu's long-conn and Slack's
Socket Mode — and it needs no publicly reachable HTTPS endpoint on the Multica
side, which outgoing webhooks would.

Rejected: outgoing webhooks / slash commands (needs a public ingress, and
Mattermost outgoing webhooks do not fire in DMs), REST polling (strictly worse
than an available push transport).

### 2. Install: server URL + bot access token

Mattermost is self-hosted, so there is no single API host to hard-code and no
hosted OAuth client. The admin creates a bot account in the System Console,
generates an access token, and pastes two fields.

The install is validated live against `GET /api/v4/users/me`, which also yields
the bot's user id and username — the username is what `@`-mention detection
needs.

Bot user ids are only unique *per server*, so the generic
`(channel_type, config->>'app_id')` routing slot cannot hold a bare bot id.
The key is `<host><path>:<bot_user_id>`, derived from the canonicalized server
URL. That keeps the existing unique index meaningful: one bot on one server maps
to one agent across all Multica workspaces.

Both `http://` and `https://` are accepted, because internal instances are the
common self-hosted case. Redirects are **not** followed, on any request: a
redirect would otherwise bounce the bearer token to a host the admin did not
name.

### 3. Group addressing: mention, or a thread the bot rooted

A DM ingests every message. A channel or group message is addressed to the bot
when it carries an explicit `@botusername`, or when it is a reply in a thread
whose root post the bot authored.

The root-author check costs one `GET /api/v4/posts/{root_id}` per thread, cached
in a bounded in-memory map, and only runs when the message is in a group, is not
already mentioned, and has a `root_id`.

Not doing: "the bot has posted anywhere in the thread". That is what users
actually expect, but it needs a thread fetch per unaddressed reply and, more
importantly, it is session state. The Slack adapter's own comment argues that
belongs in the session-aware resolver layer rather than in per-connection
adapter memory. Follow-up, not v1.

### 4. Reply delivery: one message on `EventChatDone`

Modeled on `slack/outbound.go` (~240 lines), not `telegram/outbound.go` (~1100
lines, almost all of it rate-limit scheduling that a self-hosted server does not
need).

Mattermost supports `PUT /api/v4/posts/{id}`, so streaming the reply by editing
a placeholder post is feasible later. It is deliberately out of v1.

### 5. No typing indicator

Mattermost's typing signal is the WebSocket `user_typing` action; there is no
REST endpoint. Delivering it needs a per-connection sender registry like
WeCom's, and it silently no-ops whenever the WS lease sits on a different
replica from the one handling the message. Not worth the coupling in v1, so
`CapTypingIndicator` is not declared and the `ResolverSet` carries no
`TypingNotifier`.

### 6. Text only

Matches Slack v1 and Telegram v1. A post with a non-empty `message` is text even
when it also carries files. A post with no text and only attachments is
classified by its files and answered with a courteous "cannot handle this yet"
notice, but only in a DM or when explicitly addressed, so the bot stays quiet in
busy channels.

File ingest needs the media resolver plus object storage. Follow-up.

### 7. Full surface in one pass

Backend, wiring, migration, frontend and docs land together so the feature is
usable when it merges.

### 8. Community-maintained

Mattermost joins the community-maintained table alongside DingTalk, WeCom and
Telegram. The core team does not run a Mattermost server, so the same support
boundary applies. Maintainer is recorded as unassigned until someone claims it;
the package doc points at the docs page rather than naming a person.

## Architecture

### Package layout

`server/internal/integrations/mattermost/`

| File | Responsibility |
| --- | --- |
| `config.go` | Package doc, `TypeMattermost`, stored config shape, credential decode, server-URL canonicalization, the `app_id` routing key |
| `api.go` | REST client: `GetMe`, `GetPost`, `CreatePost`, `UpdatePost`. Typed `apiError`, no redirect following |
| `ws.go` | WebSocket envelope types and the `posted` event decode |
| `mattermost_channel.go` | The `channel.Channel`: dial, authenticate, read loop, dispatch, factory, `RegisterMattermost` |
| `inbound.go` | `posted` event to `channel.InboundMessage`: chat-type mapping, mention parsing, text normalization, quoted-reply enrichment |
| `resolvers.go` | The `engine.ResolverSet`: installation routing, identity, dedup, session binding, audit |
| `install.go` | `InstallService`: live validation, at-rest encryption, upsert, list / get / revoke |
| `binding.go` | `BindingTokenService`: mint and redeem the account-link token |
| `replier.go` | `engine.OutboundReplier`: binding prompt, offline / archived notices, command confirmations, `/issue` outcomes |
| `sender.go` | Outbound post delivery: chunking, threading, user-facing copy |
| `outbound.go` | `EventChatDone` subscriber that delivers the agent's reply |

Every file is small enough to hold in one context, and each has one job. The
split matches the Telegram and Slack adapters, so a reader who knows one knows
this one.

### Inbound data flow

```
Mattermost WS  ->  mattermostChannel.Connect read loop
               ->  inboundFromPosted()            (normalize)
               ->  channel.InboundHandler         (engine.Router)
               ->  ResolverSet                    (route, dedup, bind, persist)
               ->  TaskService                    (enqueue the agent run)
```

### Outbound data flow

```
EventChatDone  ->  Outbound.processEvent
               ->  channel_task_delivery lookup   (which binding, which revision)
               ->  installation + decrypted token
               ->  sender.Send                    (chunk, thread, POST /posts)
               ->  provenance rows
```

### Session isolation

Copied from Slack, because Mattermost's channel/thread model is the same shape:

- DM: binding key is the channel id, one continuous session.
- Channel or group DM: binding key is `channel:threadRoot`, so two `@bot`
  threads in one channel are two independent sessions. The thread root is the
  inbound `root_id`, or the post's own id when a top-level mention starts a new
  thread.

The real channel id is persisted in the binding config so the outbound path can
still post when the key is composite.

### Message identity

Mattermost post ids are globally unique on a server, so `MessageID` is the post
id with no compositing. Dedup keys on `(installation, post id)`, which absorbs
the redelivery that follows a reconnect.

### Capabilities

`CapText | CapThreadReply`. Not `CapRichCard` (no Block Kit equivalent in use),
not `CapAttachment` (v1 is text), not `CapTypingIndicator` (see decision 5),
not `CapMessageEdit` (the API supports it but nothing in v1 calls it).

## Database

One widened CHECK, in the two-migration NOT VALID / VALIDATE shape the repo uses
for this constraint:

- `444_issue_origin_mattermost_chat` — drop and re-add
  `issue_origin_type_check` with `mattermost_chat` added, `NOT VALID`.
- `445_issue_origin_mattermost_chat_validate` — `VALIDATE CONSTRAINT`.

No new table, no index, no foreign key.

## API

Mirrors the Telegram routes exactly.

| Method | Path | Access |
| --- | --- | --- |
| `GET` | `/api/workspaces/{id}/mattermost/installations` | workspace member |
| `POST` | `/api/workspaces/{id}/mattermost/install?agent_id=…` | owner / admin |
| `DELETE` | `/api/workspaces/{id}/mattermost/installations/{installationId}` | owner / admin |
| `POST` | `/api/mattermost/binding/redeem` | authenticated, not workspace-scoped |

The list response never includes the encrypted token. Two realtime events,
`mattermost_installation:created` and `mattermost_installation:revoked`,
invalidate the frontend query.

## Configuration

`MULTICA_MATTERMOST_SECRET_KEY` gates the whole integration: it is the at-rest
key for the bot access token. Unset means no factory is registered and the
handlers report the integration as unconfigured. This matches every other
adapter and needs no other operator config, because the server URL is
per-installation.

## Frontend

Telegram parity:

- `packages/core/types/mattermost.ts`, `packages/core/mattermost/queries.ts`,
  API client methods, zod schemas with `EMPTY_*` fallbacks, realtime
  invalidation.
- `packages/views/settings/components/mattermost-tab.tsx` (settings panel plus
  the per-agent connect button), `mattermost-mark.tsx` (brand mark), entries in
  `integration-channel-icon.tsx` and both `integrations-tab.tsx` files.
- `packages/views/mattermost/bind-page.tsx` and
  `apps/web/app/mattermost/bind/page.tsx` for `/mattermost/bind?token=…`.
- Four locales: `en`, `zh-Hans`, `ja`, `ko`.

The connect dialog takes two fields rather than Telegram's one, which is the
only visible shape difference.

## Error handling

- **Install** distinguishes three outcomes the operator acts on differently:
  the server rejected the credential (401 — rotate the token), Multica could not
  reach the server (network or URL wrong — nothing was saved), and the bot is
  already connected to another agent or workspace (disconnect it there first).
  Nothing is persisted unless validation succeeded.
- **Connection** failures return from `Connect` and the Supervisor reconnects
  under backoff. A failed authentication challenge is returned as a distinct
  error so the log says "token rejected", not "socket closed".
- **Inbound** distinguishes infrastructure failure (returned, so the Supervisor
  reconnects and the un-acked posts redeliver into dedup) from a product drop
  (nil, audited via `RecordChannelInboundDrop`).
- **Outbound** failures are logged, never propagated into the event bus publish
  call site, and run under a bounded context so a hung server cannot wedge it.
- **Tokens never reach a log line.** The REST client's error type omits the
  request URL, because Mattermost URLs carry no token but transport errors can
  quote headers; the same discipline as the Telegram client.

## Testing

Go, table-driven, no live network:

- `config_test.go` — URL canonicalization, the `app_id` key, credential decode,
  malformed config.
- `inbound_test.go` — chat-type mapping, self and bot-post suppression, system
  message suppression, mention matching including punctuation and near-miss
  usernames, mention stripping, `/clear` and `/issue` passthrough, thread and
  quoted-reply enrichment.
- `sender_test.go` — chunking on rune boundaries and newline preference,
  threading, the empty-text case.
- `api_test.go` — `httptest` server: `GetMe`, `CreatePost`, error classification,
  redirect refusal.
- `channel_test.go` — a fake WS server: authentication challenge, `posted`
  dispatch, handler-error propagation, graceful cancel.
- `install_test.go` — validation ordering, conflict classification, revoke.
- `replier_test.go` — one case per outcome, group binding prompt suppression.

TypeScript: a malformed-response test for each new schema, per the repo's API
compatibility rule.

## Out of scope

Streaming replies, file ingest and delivery, the typing indicator, full thread
continuation without a mention, Mattermost slash commands registered on the
Mattermost side, and interactive message buttons. Each is additive and none
changes the contracts above.
