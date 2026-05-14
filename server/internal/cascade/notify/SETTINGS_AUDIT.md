# Notification settings audit (PUL-102 PR6, C5)

Per the plan's C5 amendment: audit existing multica notification infrastructure before deciding whether PR6 needs to ship a settings UI.

## Existing infrastructure

- **`notification_preference` table** (`migrations/064_notification_preference.up.sql`) — per `(workspace_id, user_id)`, holds a free-form `preferences JSONB`. Already used by the inbox preference UI for muting/unmuting notification types.
- **No dedicated UI for per-event toggles.** The current preference UI is a checkbox list under workspace settings; extending it for cascade-event toggles would need new view code in `apps/web/app/(workspace)/[slug]/settings/notifications/` plus matching `packages/views/settings/`.
- **No webhook-URL secret storage.** Slack webhooks today are env vars on integrations like `RESEND_API_KEY`; there is no per-workspace UI for configuring outbound integrations.

## Decision

PR6 **does NOT ship a settings UI** — that is out of scope per the C5 audit clause. Configuration flows through env vars instead:

| Env var | Maps to |
|---|---|
| `MULTICA_CASCADE_SLACK_WEBHOOK_URL` | One Slack channel for the whole deployment |
| `MULTICA_CASCADE_TELEGRAM_BOT_TOKEN` | One Telegram bot |
| `MULTICA_CASCADE_TELEGRAM_CHAT_ID` | One destination chat (private user chat or alert channel) |

This is single-tenant by construction — fine for the multica-Pulse deployment (one workspace owns the cascade) but explicitly NOT a multi-workspace solution.

## Follow-up tickets (after PR6 lands)

1. Per-workspace Slack webhook routing — extend `notification_preference` with a `slack_webhook_url` field and look it up by `workspace_id` at Bridge construction time.
2. Per-user Telegram routing — same idea on `user_id`. Adds a "Connect Telegram" flow to the settings UI.
3. Per-event opt-out — extend `preferences JSONB` with a `cascade.{loop_guard, plan_completed, stuck, conflict, amended}` boolean map, gate `Bridge.Send` on it. Requires the UI work the C5 audit deferred.

None of these block the PR4 worker — the env-var config is sufficient for the PUL-102 happy path. They are quality-of-life improvements once a second workspace adopts cascades.
