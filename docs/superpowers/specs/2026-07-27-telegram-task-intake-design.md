# Telegram Task Intake — Design

**Date:** 2026-07-27
**Branch:** `telegram-intake` (off `origin/main`)
**Status:** Approved for planning

## Goal

Let people submit tasks to Multica by messaging a Telegram bot. A message from a
**linked Multica member** becomes a Multica issue — attributed to that member —
assigned to a bound agent, which then starts working on it automatically (the
channel framework's default behavior).

This is delivered as a **new adapter inside the existing channel framework**
(`server/internal/integrations/channel/`), not a new subsystem. It mirrors the
Slack adapter, which was itself built as pure "implement the interface + register"
with no new database tables.

## Scope (v1)

A workspace admin pastes a Telegram bot token; the install binds to one workspace +
one agent. Any Telegram user who links their account to a Multica member (via the
Slack-style token flow) can then submit issues, attributed to that member and
assigned to the bound agent. Unlinked senders are prompted to link; non-members
cannot link.

### In scope
- Telegram `channel.Channel` adapter with webhook-based inbound.
- Public webhook endpoint that verifies Telegram's secret token and routes to the
  correct installation.
- **Per-Telegram-user → Multica-member account linking** with real per-person
  attribution, reusing the framework's `channel_user_binding` + `channel_binding_token`
  and the engine identity seam. Bot-initiated (Slack-mirror) link direction:
  unlinked sender is prompted with a one-time `/telegram/bind?token=…` link.
- Issue creation via the existing engine `Router` → `IssueService.Create`, attributed
  to the linked member and assigned to the bound agent, auto-enqueued to run.
- Outbound "issue created ✓ (link)" confirmation back into the Telegram chat.
- BYO-token config UI (per-agent connect + workspace settings list/revoke),
  modeled on Slack.

### Non-goals (v1) — natural later additions on the same foundation
- Structured/multi-field conversational intake.
- Per-chat or per-command routing rules.
- **Multica-initiated** deep-link linking (`t.me/<bot>?start=<token>` from a
  "Connect Telegram" button in the profile) and surfacing Telegram as a connected
  account in the profile UI. v1 uses the lower-footprint bot-initiated flow.
- A free-text "Telegram ID" profile field (rejected: self-attested/spoofable, no
  precedent — see Constraints).
- Sender allowlist config (superseded: linking + workspace membership is the gate).
- Long-polling delivery.
- Hosted Telegram OAuth / bot provisioning.

## Constraints & decisions

- **Upstream-clean PR.** `multica-ai/multica` is both origin and upstream. This
  work lives on `telegram-intake`, branched from a fresh `origin/main`, so the PR
  diff contains only Telegram work. The channel framework already exists upstream,
  so the PR does not drag it in. `.mcp.json` stays untracked and is never committed
  on this branch.
- **Creator attribution:** created issues are attributed to the **real linked
  member**, resolved via `channel_user_binding` through the engine identity seam
  (same as Slack). No installer fallback and no allowlist. This is verified
  attribution — the token is delivered through the Telegram account (proving control
  of the Telegram id) and redeemed by a logged-in Multica user (proving the Multica
  id), and redemption is workspace-membership-gated.
- **Rejected: Telegram id on the user profile.** A free-text profile field is
  self-attested and spoofable (a user could claim another person's Telegram id and
  receive their attribution), and there is zero precedent for storing an external
  chat id on the `user`/`member` record. The framework's `channel_user_binding`
  achieves the same "any member can self-link and submit" outcome, verified, with
  less new code.
- **No new DB tables or migrations.** Reuse `channel_installation`,
  `channel_user_binding`, `channel_binding_token`, and existing sqlc queries. Route
  inbound by `(channel_type='telegram', config->>'app_id')`, which reuses
  `GetChannelInstallationByAppID`.
- **Webhook, not receive-loop.** Unlike Slack's Socket Mode, Telegram inbound
  arrives over HTTP webhook. The framework allows this: inbound is deliberately not
  on the `Channel` interface — the adapter owns how it receives.

## Architecture

New package `server/internal/integrations/telegram/`, alongside `slack/` and
`lark/`. Additions:

- `TypeTelegram channel.Type = "telegram"`.
- A `channel.Channel` implementation (`Type`, `Connect`, `Disconnect`, `Send`,
  `Capabilities`).
  - `Connect()` registers the webhook with Telegram (`setWebhook` with URL +
    secret token) and is otherwise idle — no long-running receive loop.
  - `Disconnect()` clears the webhook (`deleteWebhook`).
  - `Send()` posts via the Telegram Bot API `sendMessage` (Markdown), chunking
    over Telegram's message-length cap.
  - `Capabilities()` set to what Telegram supports (text in/out; refine during
    implementation).
- A `ResolverSet` and a `Factory`, registered in `server/cmd/server/router.go`
  next to the Slack wiring (same secretbox encryption service for tokens).

### `config` JSONB shape (in `channel_installation.config`)
- `app_id` — the numeric bot id (the prefix of the bot token before `:`); the
  inbound routing key.
- `bot_token_encrypted` — bot token, encrypted at rest via the same secretbox used
  for Slack tokens.
- `webhook_secret` — the secret token registered with `setWebhook` and verified on
  each inbound request.

(No allowlist — access is gated by whether the sender is a linked Multica member.)

## Inbound flow

1. **Route:** `POST /api/webhooks/telegram/{botId}` — public/unauthenticated, added
   next to the GitHub/Stripe webhooks in `router.go`. Verifies the
   `X-Telegram-Bot-Api-Secret-Token` header against the installation's
   `webhook_secret`.
2. **Installation lookup:** by `(channel_type='telegram', config->>'app_id'=botId)`
   via the existing `GetChannelInstallationByAppID` query. Reject if not found or
   revoked.
3. **Normalize → engine:** build a `channel.InboundMessage` from the Telegram
   update and hand it to `engine.Router.Handle`. The engine runs its ordered
   pipeline: dedup claim → group `@bot` filter → **identity resolution** → ensure
   session → `/issue` parse → create issue → debounced agent run.
4. **Identity resolution (attribution + gate):** the Telegram `IdentityResolver`
   looks up `channel_user_binding` by `(installation_id, telegram_user_id)`.
   - **Bound:** re-verifies workspace membership, returns the member's
     `multica_user_id`. The issue is created attributed to that member and assigned
     to the installation's `agent_id`; since it's agent-assigned and not `backlog`,
     `IssueService.Create` auto-enqueues the agent. Message text becomes the issue
     title/description (framework's existing text→issue behavior).
   - **Unbound:** returns `ErrSenderUnbound` → outcome `NeedsBinding`; the message
     is not turned into an issue. See "Account linking flow."

## Account linking flow (bot-initiated, Slack-mirror)

Reuses the framework's token machinery — no new tables.

1. An unlinked Telegram user messages the bot → engine returns `NeedsBinding`.
2. The Telegram `OutboundReplier` mints a single-use, 15-minute
   `channel_binding_token` (only its SHA-256 hash stored) and replies in-chat with a
   link: `<APP_URL>/telegram/bind?token=…` ("link your account, expires in 15 min").
3. The user opens it. The bind page requires them to be **logged into Multica**
   (redirects through `/login?next=/telegram/bind?token=…` if not).
4. `POST /api/telegram/binding/redeem` — identity is taken from the **authenticated
   session** (never the token); the endpoint is **workspace-membership-gated**
   (non-member → 403) and single-use (atomic consume). It writes the
   `channel_user_binding` row `(installation_id, telegram_user_id) → multica_user_id`.
5. Subsequent messages from that Telegram user resolve to the member and create
   attributed issues.

Error mapping mirrors Slack: 410 invalid/consumed/expired token, 409 already bound
to a different user, 403 not a workspace member.

## Outbound flow

`Channel.Send` posts to the Telegram chat via `sendMessage`. The engine reply seam
(the same one Slack uses for "issue created" confirmations) posts the created-issue
notice with a link back into the originating chat. This reuses the framework's
outbound path and gives agent progress replies for free in later iterations.

## Config & management surface

Reuse `channel_installation` verbatim. New HTTP handlers, modeled on Slack's:

- `RegisterTelegramBYO` — `POST /api/workspaces/{id}/telegram/install/byo?agent_id=…`
  — accepts the pasted bot token, verifies it against Telegram (`getMe`), derives
  `app_id`, generates a `webhook_secret`, encrypts the token, upserts the
  installation, and registers the webhook. Owner/admin only.
- `ListTelegramInstallations` — `GET /api/workspaces/{id}/telegram/installations`
  — member-visible.
- `RevokeTelegramInstallation` — `DELETE /api/workspaces/{id}/telegram/installations/{installationId}`
  — flips `status='revoked'`, clears the webhook. Owner/admin only.
- `RedeemTelegramBindingToken` — `POST /api/telegram/binding/redeem` — account-link
  redemption (session identity, membership-gated), mirroring
  `RedeemSlackBindingToken`.

Frontend, modeled on Slack:
- Per-agent **Integrations** tab: paste bot token to connect
  (`packages/views/agents/components/tabs/integrations-tab.tsx` is the Slack
  precedent).
- Workspace settings **Integrations** panel: list + disconnect
  (`packages/views/settings/components/` Slack tab precedent).
- **Bind page:** `apps/web/app/telegram/bind/page.tsx` →
  `packages/views/telegram/bind-page.tsx` (mirror of the Slack bind page — reads
  `?token=`, requires auth, POSTs to the redeem endpoint, renders done/error).
- API client + query keys in `packages/core/` (Telegram equivalent of
  `packages/core/slack/queries.ts` + types).

## Error handling

- Invalid/absent secret token header → `401`/drop, audited.
- Unknown or revoked installation → drop, audited.
- Unlinked sender → `NeedsBinding`: replied once with a bind link, dedup-marked so a
  replay does not re-prompt. Non-members who attempt redemption are rejected (403) at
  the redeem endpoint, so they never get a binding.
- Redeem token errors → 410 invalid/consumed/expired, 409 bound to another user, 403
  not a member (Slack-mirror mapping).
- Malformed Telegram update JSON → parsed defensively, dropped with audit; never
  panics.
- `setWebhook`/`getMe`/`sendMessage` failures during install → surfaced to the
  admin in the config UI; install is not persisted as active if webhook
  registration fails.
- Telegram delivers updates at-least-once; the existing
  `channel_inbound_message_dedup` two-phase idempotency prevents duplicate issues.

## Testing

Go (`server/internal/integrations/telegram/` + handler tests):
- Webhook handler: valid secret from a **linked** member creates an attributed
  issue; wrong/missing secret is rejected; malformed update is dropped without panic.
- **Unlinked sender → `NeedsBinding`**: no issue created; bind link replied once.
- **Redeem flow**: happy path writes the binding; expired/consumed → 410; already
  bound to another user → 409; non-member → 403; single-use is atomic.
- Config → credentials round-trip (encrypt/decrypt, `app_id` derivation from
  token).
- Resolver lookup by bot id; identity resolver bound vs unbound.
- Inbound → `IssueService.Create` path with a **fake agent** (per repo testing
  rules — never resolve/execute real agent CLIs in default tests).
- Dedup: duplicate delivery of the same update yields one issue.
- Adapter unit tests modeled on the `slack/` tests.

Frontend (`packages/views/*.test.tsx`, `packages/core/*.test.ts`):
- Config UI connect/list/revoke; malformed-response test for the new schema per the
  API-compatibility rules.
- Bind page: needs-auth redirect, redeem success, and error states (410/409/403).

## Open items to resolve during planning

- Exact `Capability` bitmask for Telegram.
- Whether Telegram supports the Slack `FindReusableChannelUserBinding`
  cross-installation reuse, or bindings are strictly per-installation (Telegram has
  no "team" equivalent — likely per-installation only).
- Precise mapping of a Telegram update to `InboundMessage` fields (group vs DM,
  `@bot` mention handling in groups — the engine already has a group `@bot` filter
  step to reuse), and how the sender's Telegram user id is carried as `SenderID`.
