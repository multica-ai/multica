# Telegram Task Intake — Design

**Date:** 2026-07-27
**Branch:** `telegram-intake` (off `origin/main`)
**Status:** Approved for planning

## Goal

Let people submit tasks to Multica by messaging a Telegram bot. A message from an
approved sender becomes a Multica issue assigned to a bound agent, which then
starts working on it automatically (the channel framework's default behavior).

This is delivered as a **new adapter inside the existing channel framework**
(`server/internal/integrations/channel/`), not a new subsystem. It mirrors the
Slack adapter, which was itself built as pure "implement the interface + register"
with no new database tables.

## Scope (v1)

Level-1 "minimal binding": a workspace admin pastes a Telegram bot token, the
install binds to one workspace + one agent, and every message from an allowlisted
Telegram sender becomes an issue assigned to that agent.

### In scope
- Telegram `channel.Channel` adapter with webhook-based inbound.
- Public webhook endpoint that verifies Telegram's secret token and routes to the
  correct installation.
- Sender allowlist stored in installation config; non-allowlisted senders dropped
  (and audited).
- Issue creation via the existing engine `Router` → `IssueService.Create`, assigned
  to the bound agent, auto-enqueued to run.
- Outbound "issue created ✓ (link)" confirmation back into the Telegram chat.
- BYO-token config UI (per-agent connect + workspace settings list/revoke),
  modeled on Slack.

### Non-goals (v1) — natural later additions on the same foundation
- Structured/multi-field conversational intake.
- Per-chat or per-command routing rules.
- Per-Telegram-user → Multica-member account linking (real per-person attribution).
- Long-polling delivery.
- Hosted Telegram OAuth / bot provisioning.

## Constraints & decisions

- **Upstream-clean PR.** `multica-ai/multica` is both origin and upstream. This
  work lives on `telegram-intake`, branched from a fresh `origin/main`, so the PR
  diff contains only Telegram work. The channel framework already exists upstream,
  so the PR does not drag it in. `.mcp.json` stays untracked and is never committed
  on this branch.
- **Creator attribution:** v1 attributes created issues to the **installer** (the
  admin who set up the bot). The allowlist controls who may submit. Per-user
  attribution is deferred (framework supports it later via account linking).
- **No new DB tables or migrations.** Reuse `channel_installation` and existing
  sqlc queries. Route inbound by `(channel_type='telegram', config->>'app_id')`,
  which reuses `GetChannelInstallationByAppID`.
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
- `allowlist` — array of approved Telegram user/chat ids.

## Inbound flow

1. **Route:** `POST /api/webhooks/telegram/{botId}` — public/unauthenticated, added
   next to the GitHub/Stripe webhooks in `router.go`. Verifies the
   `X-Telegram-Bot-Api-Secret-Token` header against the installation's
   `webhook_secret`.
2. **Installation lookup:** by `(channel_type='telegram', config->>'app_id'=botId)`
   via the existing `GetChannelInstallationByAppID` query. Reject if not found or
   revoked.
3. **Allowlist gate:** drop the update unless the sender's Telegram user/chat id is
   in `config.allowlist`. Drops are recorded via the existing
   `channel_inbound_audit`.
4. **Normalize → engine:** build a `channel.InboundMessage` from the Telegram
   update and hand it to `engine.Router.Handle`. The engine dedups, ensures a chat
   session, parses `/issue` (or treats the message as issue intake per framework
   behavior), and calls `Router.createIssue` → `IssueService.Create`, assigning the
   issue to the installation's `agent_id`. Because the issue is assigned to an agent
   and not in `backlog`, `IssueService.Create` auto-enqueues the agent to start
   working.
   - Message text becomes the issue title/description (following the framework's
     existing text→issue behavior).
   - **Creator:** the installer (resolved from the installation), per the v1
     attribution decision.

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
- Allowlist editing is part of the install config (edit the installation's
  `allowlist`). Handler for updating allowlist (owner/admin only) — either folded
  into a config-update handler or a dedicated endpoint; decided during planning.

Frontend, modeled on Slack:
- Per-agent **Integrations** tab: paste bot token to connect
  (`packages/views/agents/components/tabs/integrations-tab.tsx` is the Slack
  precedent).
- Workspace settings **Integrations** panel: list + disconnect + edit allowlist
  (`packages/views/settings/components/` Slack tab precedent).
- API client + query keys in `packages/core/` (Telegram equivalent of
  `packages/core/slack/queries.ts` + types).

## Error handling

- Invalid/absent secret token header → `401`/drop, audited.
- Unknown or revoked installation → drop, audited.
- Non-allowlisted sender → drop, audited (no reply, to avoid confirming the bot to
  strangers).
- Malformed Telegram update JSON → parsed defensively, dropped with audit; never
  panics.
- `setWebhook`/`getMe`/`sendMessage` failures during install → surfaced to the
  admin in the config UI; install is not persisted as active if webhook
  registration fails.
- Telegram delivers updates at-least-once; the existing
  `channel_inbound_message_dedup` two-phase idempotency prevents duplicate issues.

## Testing

Go (`server/internal/integrations/telegram/` + handler tests):
- Webhook handler: valid secret creates an issue; wrong/missing secret is rejected;
  non-allowlisted sender is dropped; malformed update is dropped without panic.
- Config → credentials round-trip (encrypt/decrypt, `app_id` derivation from
  token).
- Resolver lookup by bot id.
- Inbound → `IssueService.Create` path with a **fake agent** (per repo testing
  rules — never resolve/execute real agent CLIs in default tests).
- Dedup: duplicate delivery of the same update yields one issue.
- Adapter unit tests modeled on the `slack/` tests.

Frontend (`packages/views/*.test.tsx`, `packages/core/*.test.ts`):
- Config UI connect/list/revoke; malformed-response test for the new schema per the
  API-compatibility rules.

## Open items to resolve during planning

- Exact `Capability` bitmask for Telegram.
- Whether allowlist editing is a dedicated endpoint or folded into a config-update
  handler.
- Precise mapping of a Telegram update to `InboundMessage` fields (group vs DM,
  `@bot` mention handling in groups — the engine already has a group `@bot` filter
  step to reuse).
