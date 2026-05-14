# Cascade GitHub webhook setup (PUL-102)

The cascade webhook subsystem (`server/internal/webhooks/` + `server/internal/webhooks/github/`) consumes GitHub events to drive multi-PR agent cascades. This doc covers App install, secret rotation, and verification.

## Prerequisites

1. The multica server is reachable from the public internet at a stable URL (e.g. `https://multica.ai`). GitHub will not deliver to a host it cannot resolve.
2. Migrations through `073_cascade_retrigger_issue_nullable.up.sql` are applied (`make migrate-up`).
3. `MULTICA_CASCADE_WEBHOOK_ENABLED` env var is set to a truthy value (`1`, `true`, `yes`, `on`). Without it the `/webhooks/{source}` route is not registered — the App will install fine but every delivery 404s.

## GitHub App: org-wide install

We use a GitHub App (not a per-repo webhook) so new repositories under the org are auto-subscribed without per-repo configuration drift.

1. **Create the App** at `https://github.com/organizations/<org>/settings/apps/new`.
2. **Name**: `multica-cascade` (org-scoped — name is global within GitHub, so prefix with the org if `multica-cascade` is taken).
3. **Homepage URL**: `https://multica.ai`.
4. **Webhook URL**: `https://multica.ai/webhooks/github`.
5. **Webhook secret**: generate a strong random string (≥32 bytes, base64). Store in 1Password as `Pulse-env / MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT`. The multica server reads it from env at startup; rotate via the procedure below.
6. **Permissions** (read-only — multica never writes to GitHub from the webhook handler):
   - Actions: **Read** (workflow_run events)
   - Checks: **Read** (check_run events)
   - Contents: **Read** (needed to satisfy other permission grants)
   - Pull requests: **Read** (pull_request + pull_request_review events)
   - Metadata: **Read** (mandatory baseline)
7. **Subscribe to events**:
   - `workflow_run`
   - `check_run`
   - `pull_request`
   - `pull_request_review`
8. **Install** on the target org. Set scope to "All repositories" for org-wide auto-subscription; new repos under the org need no extra config.

## Verifying the install

After install, GitHub sends a `ping` event. The adapter responds 204 to pings (intentional — they carry no PR context). To verify end-to-end:

1. Open a PR on any installed repo with a branch matching `agent-N/*` or a title starting with `[PUL-N]`.
2. Trigger a failing CI run (e.g. push a commit that breaks a test).
3. Watch the multica logs for:
   - `webhooks.received_no_store` (PR2 mode — flag on but PR3 store not yet wired)
   - `webhooks.persisted` (PR3 mode — event landed in `cascade_retrigger`)
4. Confirm a row in `cascade_retrigger` with the right `event_type` (`ci_failure`).

If you see `webhooks.signature_failed { reason: signature_invalid }`, the secret env var does not match the App's webhook secret. If you see `webhooks.schema_mismatch`, GitHub bumped the event schema and the adapter needs updating — alert and escalate; do not silently work around it.

## Secret rotation procedure (90-day cadence)

Per the constraint "Webhook secret rotation. Secret в env, rotation каждые 90 дней либо после подозрения на leak" in the plan, the adapter supports zero-downtime rotation via two env vars:

- `MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT` — the secret the server uses to verify signatures.
- `MULTICA_GITHUB_WEBHOOK_SECRET_PREVIOUS` — accepted alongside `current` during the rotation window.

Rotation steps:

1. **Generate a new secret** (≥32 random bytes, base64-encoded).
2. **Update the App's webhook secret** in `https://github.com/organizations/<org>/settings/apps/multica-cascade` to the new value. GitHub will start signing with the new secret immediately.
3. **Concurrently, in env**: set `MULTICA_GITHUB_WEBHOOK_SECRET_PREVIOUS = <old value>`, `MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT = <new value>`. Roll the server pod / process so it picks up the new env.
4. **Wait 24 hours.** GitHub's webhook delivery retry window is 8h; 24h is a safety margin. In-flight retries from the old-secret world will still verify against `_PREVIOUS`.
5. **Drop `_PREVIOUS`** from env after 24h. Roll again.
6. **Update 1Password** with the new secret value. Note the rotation date so the 90-day cadence stays predictable.

If a leak is suspected, run the rotation immediately and skip the 24h window — accept potential delivery loss; GitHub's 8h retry will recover most of it.

## Local development

The webhooks subsystem stays disabled on dev boxes by default — `MULTICA_CASCADE_WEBHOOK_ENABLED` should not be in the standard `.env`. To test the GitHub adapter locally:

1. Set `MULTICA_CASCADE_WEBHOOK_ENABLED=true` in `.env.local`.
2. Set `MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT=test-secret-anything` in `.env.local`.
3. Use `smee.io` or `ngrok` to forward GitHub deliveries to your localhost.
4. Verify with `go test ./internal/webhooks/...` — the unit tests cover the parsing matrix without needing a real GitHub.

If `_CURRENT` is unset, the adapter falls back to the PR2 stub which returns 204 on every payload — useful for testing the rest of the multica stack without configuring the adapter.
