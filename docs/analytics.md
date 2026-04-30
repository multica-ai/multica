# Product Analytics

This document is the source of truth for the analytics events Multica ships
to Amplitude. Events feed the user → story → PR funnel that tracks how many
stories users work on and how many of those ship PRs.

See [MUL-1122](https://github.com/multica-ai/multica) for the design context.

## Configuration

All analytics shipping is toggled by environment variables (see `.env.example`):

| Variable | Meaning | Default |
|---|---|---|
| `AMPLITUDE_API_KEY` | Amplitude project API key. Empty = no events are shipped. | `""` |
| `AMPLITUDE_HOST` | Amplitude API host (US or EU). | `https://api2.amplitude.com` |
| `ANALYTICS_DISABLED` | Set to `true`/`1` to force the no-op client even when `AMPLITUDE_API_KEY` is set. | `""` |

Local dev and self-hosted instances run with `AMPLITUDE_API_KEY=""`, so **no
events leave the process unless the operator explicitly opts in**.

### Self-hosted instances

Self-hosters should **never inherit a Multica-issued `AMPLITUDE_API_KEY`** —
that would route their users' behavior to our analytics project. The
defaults guarantee this:

- `.env.example` ships `AMPLITUDE_API_KEY=` empty. The Docker self-host
  compose does not set a default either.
- With the key unset, `NewFromEnv` returns `NoopClient` and logs
  `analytics: AMPLITUDE_API_KEY not set, using noop client` at startup — a
  visible confirmation that nothing is shipped.
- Operators who want their own analytics can set `AMPLITUDE_API_KEY` and
  `AMPLITUDE_HOST` to point at their own Amplitude project.
- The frontend receives the key via `/api/config`, so
  self-hosts' blank server config also disables frontend event shipping
  automatically — no separate frontend opt-out plumbing required.

## Architecture

```
handler → analytics.Client.Capture(Event)   ← non-blocking, returns immediately
                    │
                    ▼
           bounded queue (1024 events)
                    │
                    ▼
     background worker: batch + POST /2/httpapi
                    │
                    ▼
                Amplitude
```

- `analytics.Capture` is **never allowed to block a request handler**. A
  broken backend must not degrade the product — when the queue is full,
  events are dropped and counted (visible via `slog` + the `dropped` counter
  on shutdown).
- Batches flush either when `BatchSize` is reached or every `FlushEvery`
  (default 10 s), whichever comes first.
- `Close()` drains remaining events during graceful shutdown. Called from
  `server/cmd/server/main.go` via `defer`.

## Identity model

- **`user_id`** — always the user's UUID for logged-in events. The
  frontend's `amplitude.setUserId(user.id)` sets the identity so all
  events are attributed to the correct user.
- **`workspace_id`** — added to every event as an event property when present.
- **PII** — events carry `email_domain` (e.g. `gmail.com`), not the full
  email. Full email is stored once in user properties via `$setOnce` so
  it's available for individual debugging but not broadcast with every
  event.
- **User properties (`$set`)** — use for mutable cohort signals
  (role, use_case, team_size, platform_preference) that a user can
  legitimately change during onboarding. `Event.Set` on the backend
  maps to Amplitude's `$set` user property operation; the frontend helper is
  `setPersonProperties()` in `@multica/core/analytics`. Use
  `$setOnce` only for values that must never be overwritten (email,
  initial attribution, first-completion timestamp).

## Core funnel: User → Story → PR

The primary analytics goal is tracking:
1. **Who** is using the product (user identity)
2. **How many stories** each user works on
3. **How many of those stories ship PRs**

### `story_created`

Fires when a new issue (story) is created. This is the first step of the
user → story → PR funnel.

| Property | Type | Description |
|---|---|---|
| `story_id` | string (UUID) | The issue ID. |
| `creator_type` | string | `"user"` or `"agent"`. |
| `workspace_id` | string (UUID) | Added globally. |

Emitted from three sites:
- `CreateIssue` handler (user-created issues)
- `ImportStarterContent` handler (onboarding seed issues)
- `dispatchCreateIssue` in AutopilotService (autopilot-created issues)

### `pr_opened`

Fires when a pull request is linked to an issue. This closes the
user → story → PR funnel.

| Property | Type | Description |
|---|---|---|
| `story_id` | string (UUID) | The issue this PR belongs to. |
| `pr_url` | string | Full URL of the pull request. |
| `provider` | string | `"github"`, `"gitlab"`, `"bitbucket"`, etc. |
| `workspace_id` | string (UUID) | Added globally. |

**NOTE**: As of this writing, there is no inbound webhook or integration
that receives PR events from GitHub/GitLab. The event builder exists in
`server/internal/analytics/events.go` but has no emission site yet. The
emission site will be the webhook handler that processes PR creation events
from the git provider when that integration is built.

## Existing event contract

### `signup`

Fires when a new user is created. Covers both verification-code and Google
OAuth entry points (`findOrCreateUser` is the single emission site).

| Property | Type | Description |
|---|---|---|
| `email_domain` | string | Lower-cased domain portion of the user's email. |
| `signup_source` | string | Opaque attribution bundle from the frontend cookie `multica_signup_source` (UTM + referrer). Empty when the cookie is absent. |
| `auth_method` | string | Optional. `"google"` for Google OAuth signups. Absent for verification-code signups. |

User properties set with `$setOnce`:

| Property | Type | Description |
|---|---|---|
| `email` | string | Full email. Never broadcast per-event. |
| `signup_source` | string | Same as above; kept on the user for later segmentation. |

### `workspace_created`

Fires after a `CreateWorkspace` transaction commits successfully.

| Property | Type | Description |
|---|---|---|
| `workspace_id` | string (UUID) | Added globally; present here for clarity. |

**Note on "first workspace" segmentation** — we deliberately do *not* stamp
an `is_first_workspace` boolean at emit time. Computing it correctly would
require an extra column or transaction-scoped logic that still races under
concurrent creates. Instead, Amplitude answers the same question exactly by
looking at whether the user has a prior `workspace_created` event.

### `runtime_registered`

Fires the first time a `(workspace_id, daemon_id, provider)` tuple is
upserted. Heartbeats and repeat registrations never re-emit.

| Property | Type | Description |
|---|---|---|
| `runtime_id` | string (UUID) | The newly created agent_runtime row id. |
| `provider` | string | e.g. `"codex"`, `"claude"`. |
| `runtime_version` | string | Version of the agent runtime binary. |
| `cli_version` | string | Version of the `multica` CLI that registered it. |

`user_id` is the authenticated owner's user id when the daemon was
registered via a member's JWT/PAT; daemon-token registrations fall back to
`workspace:<workspace_id>` so Amplitude doesn't bucket unrelated daemons
under a single anonymous user.

### `issue_executed`

Fires **at most once per issue** — when the first task on that issue
reaches terminal `done` state.

| Property | Type | Description |
|---|---|---|
| `issue_id` | string (UUID) | |
| `task_duration_ms` | int64 | Wall-clock time between `task.started_at` and `task.completed_at`. |

`user_id` prefers the issue's human creator so agent-executed events
flow into the issue-author's user profile. Agent-created issues prefix with
`agent:` to keep Amplitude from merging the agent into a user record.

### `team_invite_sent`

Fires from `CreateInvitation` after the DB row is written.

| Property | Type | Description |
|---|---|---|
| `invited_email_domain` | string | Lower-cased domain. |
| `invite_method` | string | Currently always `"email"`. |

### `team_invite_accepted`

Fires from `AcceptInvitation`.

| Property | Type | Description |
|---|---|---|
| `days_since_invite` | int64 | Whole days from invitation creation to acceptance. |

### `onboarding_questionnaire_submitted`

Fires on the first PatchOnboarding that transitions the user's
questionnaire to all three answers present.

| Property | Type | Description |
|---|---|---|
| `team_size` | string | `solo` / `team` / `other`. |
| `role` | string | `developer` / `product_lead` / `writer` / `founder` / `other`. |
| `use_case` | string | `coding` / `planning` / `writing_research` / `explore` / `other`. |
| `team_size_has_other` | bool | |
| `role_has_other` | bool | |
| `use_case_has_other` | bool | |

User properties set with `$set`:

| Property | Type | Description |
|---|---|---|
| `team_size` | string | |
| `role` | string | |
| `use_case` | string | |

### `agent_created`

Fires on every successful `POST /api/workspaces/:id/agents`.

| Property | Type | Description |
|---|---|---|
| `agent_id` | string (UUID) | |
| `provider` | string | Runtime provider the agent is bound to. |
| `template` | string | Template slug used to seed the agent. |
| `is_first_agent_in_workspace` | bool | |

### `onboarding_completed`

Fires from CompleteOnboarding on the first call that flips
`user.onboarded_at` from NULL.

| Property | Type | Description |
|---|---|---|
| `completion_path` | string | `full` / `runtime_skipped` / `cloud_waitlist` / `skip_existing` / `unknown`. |
| `joined_cloud_waitlist` | bool | |

User properties set with `$setOnce`:

| Property | Type | Description |
|---|---|---|
| `onboarded_at` | string (RFC3339) | |

### `cloud_waitlist_joined`

Fires when a user submits the Step 3 cloud waitlist form.

| Property | Type | Description |
|---|---|---|
| `has_reason` | bool | |

### `feedback_submitted`

Fires from `CreateFeedback` after the `feedback` row is inserted.

| Property | Type | Description |
|---|---|---|
| `message_length_bucket` | string | `0-100` / `100-500` / `500-2000` / `2000+`. |
| `has_images` | bool | |
| `platform` | string | `web` / `desktop`. |
| `app_version` | string | |

### `starter_content_decided`

Fires on the atomic NULL → terminal state transition.

| Property | Type | Description |
|---|---|---|
| `decision` | string | `imported` or `dismissed`. |
| `branch` | string | `agent_guided` or `self_serve`. |

### Frontend-only events

- `[Amplitude] Page Viewed` — fired by `apps/web/components/pageview-tracker.tsx` on
  every Next.js App Router path or query-string change. Amplitude's
  auto-tracking is disabled in `initAnalytics` so we own the event shape.
- `onboarding_runtime_path_selected` — fired from
  `packages/views/onboarding/steps/step-platform-fork.tsx`.
- `onboarding_runtime_detected` — fired from
  `packages/views/onboarding/steps/step-runtime-connect.tsx`.
- `download_intent_expressed` — fired when a user clicks a CTA to `/download`.
- `download_page_viewed` — fired once per `/download` mount after OS detect.
- `download_initiated` — fired when the user clicks an installer link.
- `feedback_opened` — fired when the Feedback modal mounts.

- Attribution is NOT a separate event; UTM + referrer origin are written
  to the `multica_signup_source` cookie on the first anonymous pageview
  and read by the backend's `signup` emission.

## Governance

Before adding, renaming, or removing any event:

1. Update this document first.
2. Update `server/internal/analytics/events.go` constants and helpers to
   match.
3. PR description must state which existing funnel / insight is affected.
