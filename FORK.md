# Fork Notes

This is a self-hosting fork of [multica-ai/multica](https://github.com/multica-ai/multica). Its single invariant is:

> **No code path in this fork defaults to `*.multica.ai`.**

The fork is meant for operators who run Multica on their own infrastructure for their own users. It is not a separate product; it tracks upstream and merges in regularly.

## What's intentionally changed vs. upstream

| Area | File(s) | Change |
|---|---|---|
| CLI | `server/cmd/multica/cmd_setup.go` | `multica setup cloud` and `runSetupCloud` removed. Bare `multica setup` now configures a self-hosted server (was: cloud). `multica setup self-host` kept as a backwards-compatible alias. |
| Email | `server/internal/service/email.go` | `SendInvitationEmail` returns an error when `FRONTEND_ORIGIN` is unset (was: silently fell back to `https://app.multica.ai`). |
| Desktop | `apps/desktop/.env.production`, `apps/desktop/electron.vite.config.ts` | URLs blanked. Production builds throw if `VITE_API_URL` / `VITE_WS_URL` / `VITE_APP_URL` aren't overridden via `apps/desktop/.env.production.local`. |
| Onboarding UI | `packages/views/onboarding/**`, `apps/web/features/landing/components/download/cloud-section.tsx`, `apps/web/app/(landing)/download/download-client.tsx` | "Cloud runtime" / "Join cloud waitlist" UI surfaces removed from Step 3 (web + desktop) and from the `/download` page. |
| Docs | `README.md`, `SELF_HOSTING.md` | Top-of-README banner added; `SELF_HOSTING.md` "Switching to Multica Cloud" section removed. |

## What's intentionally NOT changed (kept for upstream-merge cleanliness)

- `packages/core/onboarding/store.ts` `joinCloudWaitlist()` and the matching API method in `packages/core/api/client.ts`.
- The `"cloud_waitlist"` member of `OnboardingCompletionPath` in `packages/core/onboarding/types.ts` (production code never produces this value, but the union still permits it).
- DB columns `user.cloud_waitlist_email` and `user.cloud_waitlist_reason`, plus the `JoinCloudWaitlist` SQL query and the `/api/me/onboarding/cloud-waitlist` server handler.
- `t.download.cloud` locale strings (now unused).
- The `noreply@multica.ai` default sender in `server/internal/service/email.go` `NewEmailService` — only used when `RESEND_FROM_EMAIL` is unset, and Resend rejects unverified domains anyway, so this is a no-op in practice. Override via `RESEND_FROM_EMAIL`.
- The marketing/docs site at `apps/docs/` — not infrastructure, doesn't phone home.
- PostHog telemetry plumbing — already off by default for self-host (no `POSTHOG_API_KEY` → `NoopClient`).

The pattern is: **delete user-facing surfaces; keep server/data schema and shared library code.** This minimizes the surface that conflicts when merging upstream.

## Merge workflow

```bash
git fetch upstream
git merge upstream/main
```

Expected conflict zones (the files this fork edits):

- `server/cmd/multica/cmd_setup.go` — re-resolve to keep `runSetupCloud` deleted and `setupCmd.RunE` pointing at `runSetupSelfHost`.
- `server/internal/service/email.go` — re-resolve to keep the `FRONTEND_ORIGIN` required path; reject any reintroduced `app.multica.ai` fallback.
- `apps/desktop/.env.production` — keep blank values.
- `apps/desktop/electron.vite.config.ts` — keep the build-time guard.
- `packages/views/onboarding/**` — re-resolve to keep cloud-waitlist UI removed; allow upstream changes to `joinCloudWaitlist` plumbing in `packages/core/` to merge cleanly.
- `apps/web/app/(landing)/download/download-client.tsx`, `apps/web/app/custom.css` — keep `CloudSection` removed.
- `README.md` — keep the self-host fork banner.

If upstream introduces a new code path that defaults to `*.multica.ai` (CLI subcommand, hardcoded URL, env var fallback, etc.), strip it before merging. Run the audit from `SELF_HOSTING.md`'s verification section after every merge:

```bash
rg -n "api\.multica\.ai|app\.multica\.ai" -g '!apps/docs' -g '!*_test.go' -g '!*.lock' -g '!CHANGELOG*' -g '!FORK.md'
```

The only acceptable matches are documentation references and the README banner.

## Building the desktop app for your server

```bash
# 1. Provide your URLs (gitignored)
cat > apps/desktop/.env.production.local <<EOF
VITE_API_URL=https://api.example.com
VITE_WS_URL=wss://api.example.com/ws
VITE_APP_URL=https://app.example.com
EOF

# 2. Build
pnpm --filter @multica/desktop build
pnpm --filter @multica/desktop package
```

Without the `.env.production.local`, the build throws — the committed `.env.production` is intentionally blank.
