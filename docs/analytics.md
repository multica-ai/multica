# Product Analytics

This document is the source of truth for the analytics events Multica ships
to PostHog. Events feed the acquisition → activation → expansion funnel that
drives our weekly Active Workspaces (WAW) north-star metric.

See [MUL-1122](https://github.com/multica-ai/multica) for the design context.

## Configuration

All analytics shipping is toggled by environment variables (see `.env.example`):

| Variable | Meaning | Default |
|---|---|---|
| `POSTHOG_API_KEY` | PostHog project API key. Empty = no events are shipped. | `""` |
| `POSTHOG_HOST` | PostHog host (US or EU cloud, or self-hosted URL). | `https://us.i.posthog.com` |
| `ANALYTICS_DISABLED` | Set to `true`/`1` to force the no-op client even when `POSTHOG_API_KEY` is set. | `""` |

Local dev and self-hosted instances run with `POSTHOG_API_KEY=""`, so **no
events leave the process unless the operator explicitly opts in**.

### Self-hosted instances

Self-hosters should **never inherit a Multica-issued `POSTHOG_API_KEY`** —
that would route their users' behavior to our analytics project. The
defaults guarantee this:

- `.env.example` ships `POSTHOG_API_KEY=` empty. The Docker self-host
  compose does not set a default either.
- With the key unset, `NewFromEnv` returns `NoopClient` and logs
  `analytics: POSTHOG_API_KEY not set, using noop client` at startup — a
  visible confirmation that nothing is shipped.
- Operators who want their own analytics can set `POSTHOG_API_KEY` and
  `POSTHOG_HOST` to point at their own PostHog project (Cloud or
  self-hosted PostHog).
- The frontend receives the key via `/api/config` (planned for PR 2), so
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
     background worker: batch + POST /batch/
                    │
                    ▼
                PostHog
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

- **`distinct_id`** — always the user's UUID for logged-in events. The
  frontend's `posthog.identify(user.id)` merges any prior anonymous events
  under the same identity, so acquisition attribution (UTM / referrer) stays
  intact across signup.
- **`workspace_id`** — added to every event as a property when present. v1
  uses event property filtering (free tier) rather than PostHog Groups
  Analytics (paid) to compute workspace-level metrics.
- **PII** — events carry `email_domain` (e.g. `gmail.com`), not the full
  email. Full email is stored once in person properties via `$set_once` so
  it's available for individual debugging but not broadcast with every
  event.
- **Person properties (`$set`)** — use for mutable cohort signals
  (role, use_case, team_size, platform_preference) that a user can
  legitimately change during onboarding. `Event.Set` on the backend
  maps to `$set`; the frontend helper is
  `setPersonProperties()` in `@multica/core/analytics`. Use
  `$set_once` only for values that must never be overwritten (email,
  initial attribution, first-completion timestamp).

## Event contract

### `signup`

Fires when a new user is created. Covers both verification-code and Google
OAuth entry points (`findOrCreateUser` is the single emission site).

| Property | Type | Description |
|---|---|---|
| `email_domain` | string | Lower-cased domain portion of the user's email. |
| `signup_source` | string | Opaque attribution bundle from the frontend cookie `multica_signup_source` (UTM + referrer). Empty when the cookie is absent. |
| `auth_method` | string | Optional. `"google"` for Google OAuth signups. Absent for verification-code signups. |

Person properties set with `$set_once`:

| Property | Type | Description |
|---|---|---|
| `email` | string | Full email. Never broadcast per-event. |
| `signup_source` | string | Same as above; kept on the person for later segmentation. |

### `workspace_created`

Fires after a `CreateWorkspace` transaction commits successfully.

| Property | Type | Description |
|---|---|---|
| `workspace_id` | string (UUID) | Added globally; present here for clarity. |

**Note on "first workspace" segmentation** — we deliberately do *not* stamp
an `is_first_workspace` boolean at emit time. Computing it correctly would
require an extra column or transaction-scoped logic that still races under
concurrent creates. Instead, PostHog answers the same question exactly by
looking at whether the user has a prior `workspace_created` event (use a
funnel with "first time user does X" or a cohort on
`person_properties.$initial_event`). No information is lost.

### `runtime_registered`

Fires the first time a `(workspace_id, daemon_id, provider)` tuple is
upserted. Heartbeats and repeat registrations never re-emit. First-time
detection uses Postgres `xmax = 0` on the upsert RETURNING clause — no
extra query, no race.

| Property | Type | Description |
|---|---|---|
| `runtime_id` | string (UUID) | The newly created agent_runtime row id. |
| `provider` | string | e.g. `"codex"`, `"claude"`. |
| `runtime_version` | string | Version of the agent runtime binary. |
| `cli_version` | string | Version of the `multica` CLI that registered it. |

`distinct_id` is the authenticated owner's user id when the daemon was
registered via a member's JWT/PAT; daemon-token registrations fall back to
`workspace:<workspace_id>` so PostHog doesn't bucket unrelated daemons
under a single "anonymous" person.

### `issue_executed`

Fires **at most once per issue** — when the first task on that issue
reaches terminal `done` state. Backed by an atomic
`UPDATE issue SET first_executed_at = now() WHERE id = $1 AND first_executed_at IS NULL RETURNING *`;
retries, re-assignments, and comment-triggered follow-up tasks all hit the
WHERE clause and no-op, so the `≥1 / ≥2 / ≥5 / ≥10` funnel buckets count
distinct issues, not tasks.

| Property | Type | Description |
|---|---|---|
| `issue_id` | string (UUID) | |
| `task_duration_ms` | int64 | Wall-clock time between `task.started_at` and `task.completed_at`. Zero when the task was created in a completed state (rare). |

`distinct_id` prefers the issue's human creator so agent-executed events
flow into the issue-author's person profile (same place `signup` and
`workspace_created` land). Agent-created issues prefix with `agent:` to
keep PostHog from merging the agent into a user record.

**Note on workspace-Nth ordinals** — we deliberately do *not* stamp
`nth_issue_for_workspace` at emit time. Computing it correctly would
require either a serialised transaction or an advisory lock per workspace;
two concurrent first-completions could otherwise both read `count=1` and
emit `n=1`. PostHog answers the same question at query time via
`row_number() OVER (PARTITION BY properties.workspace_id ORDER BY timestamp)`,
and funnel steps of the form "workspace has had ≥2 `issue_executed`
events" are expressible without the property. No information is lost.

### `team_invite_sent`

Fires from `CreateInvitation` after the DB row is written.

| Property | Type | Description |
|---|---|---|
| `invited_email_domain` | string | Lower-cased domain; full email lives in the invitation row, not the event. |
| `invite_method` | string | Currently always `"email"`. Future non-email invite flows (share link, SCIM) should pass their own value. |

`distinct_id` is the inviter's user id.

### `team_invite_accepted`

Fires from `AcceptInvitation` after both the invitation row is marked
accepted and the member row is inserted in the same transaction.

| Property | Type | Description |
|---|---|---|
| `days_since_invite` | int64 | Whole days from invitation creation to acceptance. Lets us segment "accepted same day" (warm) from "dug out of email weeks later" (cold). |

`distinct_id` is the invitee's user id — this is the event that closes the
expansion funnel.

### `onboarding_questionnaire_submitted`

Fires on the first PatchOnboarding that transitions the user's
questionnaire JSONB from "at least one slot empty" to "all three
filled" (team_size, role, use_case). Revisions past that point don't
re-emit — the funnel counts users, not edits.

| Property | Type | Description |
|---|---|---|
| `team_size` | string | `solo` / `team` / `other`. |
| `role` | string | `developer` / `product_lead` / `writer` / `founder` / `other`. |
| `use_case` | string | `coding` / `planning` / `writing_research` / `explore` / `other`. |
| `team_size_has_other` | bool | `true` when the user filled the Q1 free-text escape. |
| `role_has_other` | bool | Ditto Q2. |
| `use_case_has_other` | bool | Ditto Q3. |

Person properties set with `$set` (not once — users can go back and
change answers before submitting again):

| Property | Type | Description |
|---|---|---|
| `team_size` | string | Mirrors the event property for cohort queries. |
| `role` | string | Same. |
| `use_case` | string | Same. |

`distinct_id` is the user's id. No workspace_id — the questionnaire is
per-user, not per-workspace.

### `agent_created`

Fires on every successful `POST /api/workspaces/:id/agents`. Not
onboarding-specific — the `is_first_agent_in_workspace` property
isolates the Step 4 signal from later agent additions.

| Property | Type | Description |
|---|---|---|
| `agent_id` | string (UUID) | |
| `provider` | string | Runtime provider the agent is bound to (`claude`, `codex`, etc). |
| `template` | string | Template slug used to seed the agent (`coding` / `planning` / `writing` / `assistant`). Empty when the caller didn't come from a template picker. |
| `is_first_agent_in_workspace` | bool | `true` when the workspace had zero agents before this insert. |

`distinct_id` is the authenticated owner's user id.

### `onboarding_completed`

Fires from CompleteOnboarding on the first call that actually flips
`user.onboarded_at` from NULL. Retries are idempotent server-side but
deliberately do NOT re-emit, so the funnel counts first-completions
only. The client sends `completion_path` in the POST body to label
which exit the user took.

| Property | Type | Description |
|---|---|---|
| `completion_path` | string | One of `full` / `runtime_skipped` / `cloud_waitlist` / `skip_existing` / `unknown`. See below. |
| `joined_cloud_waitlist` | bool | Derived from `user.cloud_waitlist_email`. Orthogonal to `completion_path` — a user may submit the waitlist form and still pick CLI. |

Person properties set with `$set_once`:

| Property | Type | Description |
|---|---|---|
| `onboarded_at` | string (RFC3339) | Timestamp the first completion landed. Enables cohort queries like "users onboarded before X" directly from person_properties. |

`completion_path` values:

- `full` — Reached Step 5 (first_issue) with a runtime connected.
- `runtime_skipped` — Completed without connecting a runtime (user hit Skip in Step 3).
- `cloud_waitlist` — Submitted the cloud waitlist form and skipped Step 3.
- `skip_existing` — "I've done this before" from Welcome. The user already had a workspace.
- `unknown` — Legacy fallback when the client didn't send a path. Should stay near zero after rollout.

### `cloud_waitlist_joined`

Fires from JoinCloudWaitlist whenever a user submits the Step 3 cloud
waitlist form. Not a completion signal — it's orthogonal to the main
funnel and used to size hosted-runtime interest.

| Property | Type | Description |
|---|---|---|
| `has_reason` | bool | Presence flag for the free-text reason field. The free text stays in the DB; we don't broadcast it. |

`distinct_id` is the user's id.

### `feedback_submitted`

Fires from `CreateFeedback` after the `feedback` row is inserted and the
hourly per-user rate-limit check has passed. Retries within the same hour
that were rate-limited (429) don't emit. The free-text message is stored
in the DB and never broadcast.

| Property | Type | Description |
|---|---|---|
| `message_length_bucket` | string | `0-100` / `100-500` / `500-2000` / `2000+` — coarse bucket of `len(message)` so we can tell "quick note" from "bug report with repro steps" without leaking content. |
| `has_images` | bool | `true` when the markdown contains at least one `![...](url)` image reference — signals bug reports with visual evidence. |
| `platform` | string | Client platform from `X-Client-Platform` header (`web` / `desktop`). Omitted when the header is absent. |
| `app_version` | string | Client version from `X-Client-Version` header. Omitted when absent. |

`distinct_id` is the submitter's user id; `workspace_id` is attached from
the modal's current-workspace context and may be empty when feedback is
sent from a pre-workspace surface.

### `starter_content_decided`

Fires on the atomic NULL → terminal state transition in both
ImportStarterContent and DismissStarterContent. The `branch` property
mirrors what ImportStarterContent would emit for the same workspace,
so import-vs-dismiss rates split cleanly by branch.

| Property | Type | Description |
|---|---|---|
| `decision` | string | `imported` or `dismissed`. |
| `branch` | string | `agent_guided` (workspace had ≥1 agent at decision time) or `self_serve` (no agents). |

`distinct_id` is the user's id; `workspace_id` is attached from the
request payload.

### Frontend-only events

- `$pageview` — fired by `apps/web/components/pageview-tracker.tsx` on
  every Next.js App Router path or query-string change. The tracker
  mounts once under `WebProviders` and drives the acquisition funnel's
  `/ → signup` step. posthog-js's automatic pageview capture is
  disabled in `initAnalytics` so we own the event shape.
- `onboarding_runtime_path_selected` — fired from
  `packages/views/onboarding/steps/step-platform-fork.tsx` when the web
  user clicks one of the three Step 3 fork cards (before any server
  call happens, so it's frontend-only). Properties: `path`
  (`download_desktop` / `cli` / `cloud_waitlist`), `source` (`step3`;
  literal today but reserved for future surfaces reusing this event),
  `is_mac`. Also writes `platform_preference` (`web` / `desktop`) to
  person properties so every subsequent event on the user can be
  broken down by chosen platform. **Note**: semantic "download
  intent" is now better served by `download_intent_expressed` below —
  `path: "download_desktop"` signals Step 3 path choice specifically,
  not actual download start.

- `onboarding_runtime_detected` — fired from
  `packages/views/onboarding/steps/step-runtime-connect.tsx` (desktop
  Step 3) once per mount, when the scanning phase resolves — either
  immediately on first runtime registration, or after the 5 s empty
  timeout. Answers the question "did the user have any AI CLI
  installed on this machine when they hit Step 3" — currently
  unanswerable from the existing funnel because the bundled daemon
  fails to register at all when zero CLIs are on PATH, so
  `runtime_registered` is silent on that cohort. Splits
  `completion_path=runtime_skipped` into "had CLIs, skipped anyway"
  vs "no CLIs available, had no choice". Properties:
  - `source`: `step3_desktop` (literal; reserved for a future web
    emission under a different value).
  - `outcome`: `found` (at least one runtime registered before the
    5 s grace window expired) or `empty` (none registered by then).
  - `runtime_count`: number of runtimes visible to this user at
    resolution time.
  - `online_count`: subset of `runtime_count` whose `status` is
    `online`.
  - `providers`: sorted array of distinct provider names (e.g.
    `["claude", "codex"]`).
  - `has_claude` / `has_codex` / `has_cursor`: convenience booleans
    derived from `providers` for funnel breakdowns without array
    filtering in HogQL.
  - `detect_ms`: wall-clock ms from component mount to resolution.
    Surfaces daemon boot latency — `found` events with a high
    `detect_ms` approach the timeout threshold and inform whether
    to lengthen the grace period.

  Person properties set with `$set`:
  - `has_any_cli`: boolean — cohort signal for "user has at least
    one local AI CLI detected on this machine".
  - `detected_cli_count`: number — granular cohort signal.

  Not emitted from the web Step 3 (`step-platform-fork.tsx`) — web
  users don't run the bundled daemon, so their runtime list reflects
  daemons from other machines and would corrupt the
  "CLI installed locally" signal.

- `download_intent_expressed` — fired whenever a user clicks a CTA
  that points at the `/download` page. Surfaces five sources across
  the funnel, letting the top-of-funnel entry be split cleanly.
  Wrapper lives in `packages/core/analytics/download.ts`
  (`captureDownloadIntent`). Properties:
  - `source`: `landing_hero` / `landing_footer` / `login` / `welcome`
    / `step3`
  Also writes `platform_preference: "desktop"` to person properties.

- `download_page_viewed` — fired once per `/download` mount after OS
  detect resolves (`apps/web/app/(landing)/download/download-client.tsx`).
  Properties:
  - `detected_os`: `mac` / `windows` / `linux` / `unknown`
  - `detected_arch`: `arm64` / `x64` / `unknown`
  - `detect_confident`: `true` when detect used
    `userAgentData.getHighEntropyValues` (Chromium); `false` when it
    fell back to the UA string (Safari on Mac always lands here —
    lets us isolate the arm64-default-for-Intel risk cohort).
  - `version_available`: `false` when the GitHub API fetch failed
    and the page is in the "Version unavailable" degraded state.
  Also writes `first_detected_os` / `first_detected_arch` via
  `$set_once` so every downstream event gains a platform dimension
  without re-emitting.

- `download_initiated` — fired when the user clicks a specific
  installer link on `/download`. Both the hero CTA and the All
  Platforms matrix rows emit this; split by `primary_cta`.
  Properties:
  - `platform`: `mac` / `windows` / `linux`
  - `arch`: `arm64` / `x64`
  - `format`: `dmg` / `zip` / `exe` / `appimage` / `deb` / `rpm`
  - `version`: release tag (e.g. `v0.2.13`) — correlates adoption
    with release cadence.
  - `primary_cta`: `true` for the hero-recommended installer, `false`
    for a manual pick from the All Platforms matrix.
  - `matched_detect`: `true` when the chosen platform+arch matches
    what the page detected. `false` lets us quantify detect misses
    from the single event (no cross-join needed).
- `feedback_opened` — fired when the in-app Feedback modal mounts
  (user clicked "Feedback" in the Help launcher). Paired with the
  backend's `feedback_submitted` to give a completion rate for the
  form. Wrapper lives in `packages/core/analytics/feedback.ts`
  (`captureFeedbackOpened`). Properties:
  - `source`: `help_menu` (reserved — future entry points like
    keyboard shortcut or error-toast CTA will pass their own value)
  - `workspace_id`: string (UUID) when the modal opens inside a
    workspace. Omitted on pre-workspace surfaces.

- Attribution is NOT a separate event; UTM + referrer origin are written
  to the `multica_signup_source` cookie on the first anonymous pageview
  and read by the backend's `signup` emission. The cookie carries a JSON
  payload URL-encoded at write time (`encodeURIComponent`) and
  URL-decoded at read time (`url.QueryUnescape`) — the JSON is never
  mid-truncated; individual values are capped at 96 chars before
  `JSON.stringify`, and the entire payload is dropped if it still exceeds
  512 chars. That way PostHog sees either intact JSON or nothing at all.

## Governance

Before adding, renaming, or removing any event:

1. Update this document first.
2. Update `server/internal/analytics/events.go` constants and helpers to
   match.
3. PR description must state which existing funnel / insight is affected.
