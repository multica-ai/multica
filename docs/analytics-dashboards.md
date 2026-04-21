# PostHog Funnel Dashboards

Operational spec for the **Multica Funnel — Weekly** dashboard that feeds
[MUL-1101](https://github.com/multica-ai/multica) weekly review. Event
contract is defined in [`analytics.md`](./analytics.md); this doc defines
how those events roll up into PostHog Insights.

> This doc is the source of truth for what the dashboard should contain.
> Changing an insight's step list, window, or breakdown here counts as a
> dashboard schema change — update the doc first, then the UI.

## Scope

Five insights, one dashboard, reconciled weekly against the MUL-1101 W16
hand-calculated funnel. Target agreement: **< 5% absolute error** on every
step. Discrepancy beyond that is a bug in either the events or the query,
not a reason to move the dashboard goal posts.

No historical backfill — data starts from the event-pipeline go-live
(MUL-1122). The first weekly review that the dashboard can replace is the
first full ISO week after the go-live date.

## Conventions

All insights use the following unless noted otherwise:

- **Project**: `Multica` (US cloud, `https://us.i.posthog.com`).
- **Date range**: `Last 12 weeks` on the dashboard, `Last 8 weeks` on
  individual insights during authoring.
- **Funnel type**: `Sequential`, `First touch` attribution.
- **Conversion window**: 7 days (see each insight for exceptions).
- **Graph type**: Funnel — steps, unless marked "Trends" or "HogQL".
- **Aggregation**: `Unique users` for user-keyed funnels,
  `Unique groups / HogQL` for workspace-keyed funnels (see note below).

### Actor: user vs. workspace

PostHog's free-tier funnel aggregates by `distinct_id` (user). Three of
the insights below are naturally **workspace-level** (activation, second
issue, expansion). Until we decide whether to enable PostHog Groups as a
paid add-on, we express workspace-level funnels via **HogQL insights**
keyed on `properties.workspace_id`. The `runtime_registered` and
`issue_executed` events both carry `workspace_id` (see
[`analytics.md`](./analytics.md)), and `workspace_created` carries the
new workspace's id, so every step is addressable by that property.

If the dashboard outgrows the free tier (Groups would simplify insight 2,
3, 5), switch the three workspace-level insights to Group funnels and
delete their HogQL equivalents. That's a future decision tracked as the
"open item" at the bottom of this doc.

## Insights

### 1. Main funnel (North Star)

- **Name**: `Main funnel — signup to power user`
- **Type**: Funnel (user)
- **Conversion window**: 7 days
- **Steps**:
  1. `$pageview`
  2. `signup`
  3. `workspace_created`
  4. `runtime_registered`
  5. `issue_executed` — step filter: `Event count >= 1`
  6. `issue_executed` — step filter: `Event count >= 2`
  7. `issue_executed` — step filter: `Event count >= 5`
  8. `issue_executed` — step filter: `Event count >= 10`
- **Breakdowns** (duplicate the insight once per breakdown, keep in the
  same dashboard so weekly review sees all cuts):
  - Cohort / signup week — breakdown by
    `person.properties.$initial_timestamp` bucketed by week via HogQL, OR
    create a dynamic cohort per ISO week of signup and use
    `Compare to cohort`.
  - UTM source — breakdown by `person.properties.$initial_utm_source`.
    (posthog-js populates `$initial_*` automatically on identify merge;
    the opaque `signup_source` JSON we `$set_once` on the person is not
    natively breakdown-friendly.)
  - Runtime provider — breakdown by `event.properties.provider` on
    step 4 (`runtime_registered`). PostHog will scope the break to that
    step and carry it through subsequent steps.
  - OS — breakdown by `person.properties.$os` (auto-populated by
    posthog-js on identify).

**Note — `$pageview` step 1**: we don't restrict to `/` on purpose. Users
who land on any in-app URL (shared issue link, docs, blog) and then sign
up are still valid funnel entries. The landing-page specific acquisition
cut is insight 4.

### 2. Activation — workspace → runtime

- **Name**: `Activation — workspace_created to runtime_registered`
- **Type**: HogQL insight (see "Actor: user vs. workspace" above)
- **Aggregation**: workspace (via `properties.workspace_id`)
- **Conversion window**: 7 days

```sql
-- Weekly activation conversion: % of workspaces created in week W that
-- register a runtime within 7 days. Split by signup_source and provider
-- for the P0-2 investigation under MUL-1101.
WITH wc AS (
  SELECT properties.workspace_id     AS workspace_id,
         timestamp                   AS created_at,
         person.properties.signup_source AS signup_source,
         toStartOfWeek(timestamp, 1) AS wk
  FROM events
  WHERE event = 'workspace_created'
    AND properties.workspace_id IS NOT NULL
),
rr AS (
  SELECT properties.workspace_id AS workspace_id,
         properties.provider     AS provider,
         min(timestamp)          AS first_registered_at
  FROM events
  WHERE event = 'runtime_registered'
    AND properties.workspace_id IS NOT NULL
  GROUP BY properties.workspace_id, properties.provider
)
SELECT wc.wk                                          AS week,
       coalesce(wc.signup_source, '(none)')           AS signup_source,
       coalesce(rr.provider, '(none)')                AS provider,
       count()                                        AS workspaces,
       countIf(rr.first_registered_at IS NOT NULL
         AND rr.first_registered_at - wc.created_at <= toIntervalDay(7))
                                                     AS registered_within_7d,
       round(registered_within_7d / workspaces * 100, 1) AS conv_pct
FROM wc LEFT JOIN rr USING workspace_id
GROUP BY week, signup_source, provider
ORDER BY week DESC, workspaces DESC
```

Feeds MUL-1101 P0-2 (why 48.5% of workspaces don't register a runtime).

### 3. Second-issue specific — 1st → 2nd `issue_executed`

- **Name**: `Second-issue activation by first task duration`
- **Type**: HogQL insight (workspace-keyed)
- **Conversion window**: 7 days after the first `issue_executed` for that
  workspace.

```sql
-- Workspaces that ran ≥1 issue, split by how long the first issue took,
-- then measure conversion to ≥2. Feeds MUL-1101 P0-3.
WITH numbered AS (
  SELECT properties.workspace_id                          AS workspace_id,
         timestamp,
         properties.task_duration_ms                      AS duration_ms,
         row_number() OVER (
           PARTITION BY properties.workspace_id
           ORDER BY timestamp
         )                                                AS rn
  FROM events
  WHERE event = 'issue_executed'
    AND properties.workspace_id IS NOT NULL
),
first AS (
  SELECT workspace_id,
         timestamp AS first_ts,
         duration_ms
  FROM numbered
  WHERE rn = 1
),
second AS (
  SELECT workspace_id, timestamp AS second_ts
  FROM numbered
  WHERE rn = 2
),
bucketed AS (
  SELECT first.workspace_id,
         multiIf(
           first.duration_ms < 30000,  '<30s',
           first.duration_ms < 300000, '30s-5min',
                                       '5min+'
         ) AS first_duration_bucket,
         first.first_ts,
         second.second_ts
  FROM first
  LEFT JOIN second USING workspace_id
)
SELECT first_duration_bucket                                      AS bucket,
       count()                                                    AS had_first,
       countIf(second_ts IS NOT NULL
         AND second_ts - first_ts <= toIntervalDay(7))            AS had_second_within_7d,
       round(had_second_within_7d / had_first * 100, 1)           AS conv_pct
FROM bucketed
GROUP BY bucket
ORDER BY
  multiIf(bucket = '<30s', 1, bucket = '30s-5min', 2, 3)
```

### 4. Acquisition — landing → signup

- **Name**: `Acquisition — landing to signup`
- **Type**: Funnel (user)
- **Conversion window**: 7 days
- **Steps**:
  1. `$pageview` with property filter `$pathname = '/'`
  2. `signup`
- **Breakdowns** (separate saved insights, one per breakdown):
  - `person.properties.$initial_utm_source`
  - `person.properties.$initial_referring_domain`

  posthog-js auto-sets these on identify merge. The opaque
  `signup_source` cookie still exists server-side for debugging /
  auditing, but it's not what drives this breakdown.

Feeds MUL-1101 P1-1 (non-Trending acquisition).

### 5. Expansion — workspace → invite sent → invite accepted

- **Name**: `Expansion — workspace to team invite accepted`
- **Type**: HogQL insight (workspace-keyed — the sender and the acceptor
  are different users, so a user funnel always drops to 0% at step 3).
- **Conversion window**: 30 days (invites stay open longer than the
  core activation path).

```sql
WITH wc AS (
  SELECT properties.workspace_id AS workspace_id,
         min(timestamp)          AS created_at
  FROM events
  WHERE event = 'workspace_created'
    AND properties.workspace_id IS NOT NULL
  GROUP BY workspace_id
),
sent AS (
  SELECT properties.workspace_id AS workspace_id,
         min(timestamp)          AS first_sent_at
  FROM events
  WHERE event = 'team_invite_sent'
    AND properties.workspace_id IS NOT NULL
  GROUP BY workspace_id
),
accepted AS (
  SELECT properties.workspace_id AS workspace_id,
         min(timestamp)          AS first_accepted_at
  FROM events
  WHERE event = 'team_invite_accepted'
    AND properties.workspace_id IS NOT NULL
  GROUP BY workspace_id
)
SELECT toStartOfWeek(wc.created_at, 1) AS cohort_week,
       count()                         AS workspaces,
       countIf(sent.first_sent_at IS NOT NULL
         AND sent.first_sent_at - wc.created_at <= toIntervalDay(30))
                                       AS sent_invite_30d,
       countIf(accepted.first_accepted_at IS NOT NULL
         AND accepted.first_accepted_at - wc.created_at <= toIntervalDay(30))
                                       AS accepted_invite_30d,
       round(sent_invite_30d    / workspaces       * 100, 1) AS pct_sent,
       round(accepted_invite_30d / sent_invite_30d * 100, 1) AS pct_accepted_of_sent
FROM wc
LEFT JOIN sent     USING workspace_id
LEFT JOIN accepted USING workspace_id
GROUP BY cohort_week
ORDER BY cohort_week DESC
```

### 6. WAW — weekly active workspaces (time series)

- **Name**: `WAW — weekly active workspaces`
- **Type**: HogQL insight → line chart
- **Purpose**: the north-star time series. Lives on the dashboard header.

```sql
SELECT toStartOfWeek(timestamp, 1)            AS wk,
       uniqExact(properties.workspace_id)     AS waw
FROM events
WHERE event = 'issue_executed'
  AND properties.workspace_id IS NOT NULL
  AND timestamp >= now() - toIntervalWeek(26)
GROUP BY wk
ORDER BY wk
```

WAW definition here = "workspaces with ≥1 `issue_executed` in the week".
MUL-1101's stricter WAW (≥2 issues / week) is a second series on the
same chart; add with `uniqExactIf(properties.workspace_id, cnt >= 2)`
after a pre-agg if needed. Start with ≥1 until we're reconciled.

## Dashboard composition

- **Dashboard name**: `Multica Funnel — Weekly`
- **Dashboard description**: "Every Monday review. Source of truth for
  MUL-1101. See docs/analytics-dashboards.md for spec."
- **Tags**: `funnel`, `weekly-review`, `mul-1101`
- **Default date range**: `Last 12 weeks`
- **Refresh**: Every 4 hours (free tier cap). Manual refresh before the
  Monday review.
- **Access**: Workspace-wide read. Only maintainers edit.

Layout, top to bottom:

1. Header: `WAW — weekly active workspaces` (insight 6, full width).
2. Row of four: `Main funnel — signup to power user` (insight 1),
   variants: base + UTM source + provider + OS.
3. Row of two: `Activation — workspace_created to runtime_registered`
   (insight 2), `Second-issue activation by first task duration`
   (insight 3).
4. Row of two: `Acquisition — landing to signup` (insight 4, UTM +
   referrer variants side-by-side), `Expansion — workspace to team
   invite accepted` (insight 5).

## Reconciliation — first week after go-live

Before handing the dashboard to the weekly review, run the reconciliation
against MUL-1101 §1 W16 hand-calculated table. Pick the first full ISO
week **after** event pipeline go-live (not W16 itself — W16 predates the
events and will be empty). The goal is **structural parity**, not a
W16-for-W16 match.

For each of these five checkpoints, the dashboard and a separate SQL
query against the production database (workspaces + issues tables)
should agree within **5 percentage points**:

| Check | Dashboard source | DB-side query sketch |
|---|---|---|
| Weekly new signups | insight 1 step 2 count | `SELECT count(*) FROM users WHERE created_at >= $week_start AND created_at < $week_end` |
| Weekly new workspaces | insight 1 step 3 count | `SELECT count(*) FROM workspace WHERE created_at …` |
| → registered runtime | insight 2 `registered_within_7d` | joined count on `agent_runtime` with 7-day window |
| → ≥1 issue executed | insight 1 step 5 count | `SELECT count(DISTINCT workspace_id) FROM issues WHERE first_executed_at …` |
| → ≥2 issues executed | insight 1 step 6 count | same as above with `HAVING count >= 2` |

If any check is > 5pp off, **do not paper over it**: find the root cause
(event dropped in the pipeline, filter mis-specified, cohort overlap) and
fix the underlying issue before the dashboard replaces the hand
calculation. Update this doc if a query is wrong; update
`server/internal/analytics` if an event is wrong.

## Open items (revisit after 2–3 weeks of data)

- **PostHog Groups**: evaluate upgrading so insights 2, 3, 5 can be
  native funnel insights instead of HogQL. Trigger = HogQL queries
  hitting the 30s timeout ceiling, or the cost of re-writing three
  insights exceeds the add-on.
- **Historical backfill**: currently out of scope. Revisit only if the
  first two weekly reviews can't produce a stable trend because the
  series is too short.
- **`cli_runtime_register_succeeded`** as a double-sided check on the
  activation step: look at the actual drop rate in insight 2 before
  deciding whether the CLI-side event is worth shipping. If the drop is
  clearly "user never tried", no CLI event is needed; if it looks like
  "tried and failed silently", add the event.
