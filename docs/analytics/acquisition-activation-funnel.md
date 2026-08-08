# Acquisition-to-activation funnel

This is the implementation contract for the launch funnel. It intentionally
uses two stores:

- PostHog receives only the two anonymous website events that have no durable
  database equivalent.
- The operational database remains authoritative for workspace and task
  milestones. `ListLaunchFunnelEvents` derives those milestones for reporting;
  backend events are not duplicated into PostHog.

This preserves the product-analytics decision in MUL-4127 and avoids sending
workspace, issue, task, user, email, IP, URL path, prompt, transcript, or source
code data to an analytics vendor.

## Event dictionary

| Event | Definition | Deduplication | Timestamp source |
| --- | --- | --- | --- |
| `qualified_landing_view` | The public homepage remained visible for 3 continuous seconds in a non-WebDriver, non-bot browser. | One per tab session via `sessionStorage`; dashboard uses unique anonymous/person ID across tabs. | PostHog capture time |
| `signup_or_download_start` | An unauthenticated signup CTA or a Desktop download CTA was clicked. `intent` is `signup` or `download`; `placement` names the CTA. | Earliest event per anonymous/person ID and intent in the reporting window. | PostHog capture time |
| `workspace_created` | A workspace in the selected creation cohort. | One per workspace. | `workspace.created_at` |
| `runtime_connected` | The first persisted runtime for the workspace. | Earliest `(created_at, id)` per workspace. Repeat registration/heartbeat is excluded. | `agent_runtime.created_at` |
| `agent_created` | The first agent in the workspace. | Earliest `(created_at, id)` per workspace. | `agent.created_at` |
| `issue_assigned` | The first distinct issue that produced an agent task. A queued task is the durable assignment edge. | Earliest task per issue, then earliest issue per workspace. Retries and comment continuations are excluded. | `agent_task_queue.created_at` |
| `task_started` | The first task start for the first assigned issue. | Earliest non-null `started_at` for that issue. | `agent_task_queue.started_at` |
| `issue_in_review` | The first transition of the first assigned issue to `in_review`, after assignment. This is the activation completion edge. | Earliest matching `activity_log` row. Later status cycles are excluded. | `activity_log.created_at` |
| `second_issue_assigned` | The second distinct issue in the workspace that produced an agent task. | Second issue after per-issue task deduplication. Retries do not count. | `agent_task_queue.created_at` |

Activation is `issue_in_review <= workspace_created + 7 days`. Retention is
`second_issue_assigned <= issue_in_review + 14 days`. Time to first assignment
is `issue_assigned - workspace_created`. Assignment-start reliability is the
share of first assignments with `task_started <= issue_assigned + 2 minutes`.

## Dimensions

`source`, `medium`, and `campaign` are first-touch fields. On account creation,
the cookie is reduced to those three bounded values plus `referrer_host`; the
full URL, path, query, UTM content/term, and raw referrer are discarded. The
same attribution is inherited by workspace milestones through the earliest
workspace owner.

`runtime_family` and `provider_family` come from `agent_runtime.runtime_mode`
and `agent_runtime.provider`. `occurred_at` is always UTC. `platform` is
currently `web` on acquisition events and `unknown` on durable milestones; see
Known blind spots. `failure_reason` is populated by the terminal-failure query
below and is never populated with a raw error message.

## Dashboard queries

Use `ListLaunchFunnelEvents(from_time, to_time, excluded_workspace_ids)` as the
canonical workspace event stream. The dashboard should materialize that result
as `funnel_events` and use these definitions:

```sql
-- PostHog/HogQL acquisition panel. Dashboard filters must additionally set
-- environment = production and is_demo = false.
SELECT
  event,
  properties.source AS source,
  properties.campaign AS campaign,
  uniq(distinct_id) AS visitors
FROM events
WHERE event IN ('qualified_landing_view', 'signup_or_download_start')
  AND timestamp >= now() - INTERVAL 30 DAY
GROUP BY event, source, campaign;
```

The cross-store top-line conversion is an aggregate cohort calculation by
reporting window/source/campaign: activated workspaces within 7 days divided by
unique qualified visitors. It deliberately does not export or persist the
PostHog anonymous ID in the operational database. Show the PostHog acquisition
conversion and DB lifecycle conversion beside the aggregate end-to-end rate so
identity loss is visible rather than hidden.

```sql
-- Workspace funnel counts and conversion from workspace creation.
WITH counts AS (
  SELECT event_name, COUNT(DISTINCT workspace_id) AS workspaces
  FROM funnel_events
  GROUP BY event_name
)
SELECT event_name,
       workspaces,
       workspaces::numeric
         / NULLIF(MAX(workspaces) FILTER (
             WHERE event_name = 'workspace_created'
           ) OVER (), 0) AS conversion
FROM counts;

-- Median time to first assignment and activation.
SELECT
  percentile_cont(0.5) WITHIN GROUP (
    ORDER BY assigned.occurred_at - created.occurred_at
  ) AS median_time_to_first_assignment,
  COUNT(*) FILTER (
    WHERE reviewed.occurred_at <= created.occurred_at + interval '7 days'
  )::numeric / NULLIF(COUNT(*), 0) AS activation_rate,
  COUNT(*) FILTER (
    WHERE second.occurred_at <= reviewed.occurred_at + interval '14 days'
  )::numeric / NULLIF(COUNT(*) FILTER (WHERE reviewed.occurred_at IS NOT NULL), 0)
    AS retained_workspace_rate
FROM funnel_events created
LEFT JOIN funnel_events assigned
  ON assigned.workspace_id = created.workspace_id
 AND assigned.event_name = 'issue_assigned'
LEFT JOIN funnel_events reviewed
  ON reviewed.workspace_id = created.workspace_id
 AND reviewed.event_name = 'issue_in_review'
LEFT JOIN funnel_events second
  ON second.workspace_id = created.workspace_id
 AND second.event_name = 'second_issue_assigned'
WHERE created.event_name = 'workspace_created';
```

Use Prometheus for live reliability and platform-failure panels:

```promql
# Started within two minutes is computed from the DB event stream. This is the
# live start ratio and queue-latency guardrail.
sum(rate(multica_agent_task_started_total[1h]))
  / clamp_min(sum(rate(multica_agent_task_enqueued_total[1h])), 1)

histogram_quantile(0.95,
  sum by (le, runtime_mode) (
    rate(multica_agent_task_queue_wait_seconds_bucket[1h])
  )
)

# Terminal platform failures. Keep the canonical bounded failure_reason label;
# never use agent_task_queue.error as a label or dashboard dimension.
sum by (runtime_mode, failure_reason) (
  increase(multica_agent_task_failed_total[24h])
)
```

Recommended panels are funnel conversion, median time to first assignment,
7-day activation, 14-day second-issue retention, assignment start ratio and
p95 queue wait, and terminal failures by runtime/failure reason. Every panel
must support source/campaign and runtime/provider filters where the underlying
source provides them.

## Exclusions and privacy

- Pass every non-production/test workspace UUID through
  `excluded_workspace_ids`; keep the list in dashboard configuration, not code.
- Filter PostHog to `environment = production` and `is_demo = false`.
- Exclude authenticated visits from `qualified_landing_view` conversion when
  building the acquisition funnel.
- Do not infer missing self-hosted events or backfill them from network traffic.
- Do not export raw IDs outside the internal Product/Data dashboard. IDs exist
  in the event-stream query only for deterministic joins and deduplication.

## Validation procedure

1. Use one production-configured test account whose workspace UUID is already
   present in `excluded_workspace_ids`; never use a real customer workspace.
2. Open `/?utm_source=validation&utm_medium=test&utm_campaign=har14` in a new
   browser session. Keep the page visible for 3 seconds and click one signup or
   download CTA. Confirm exactly one `qualified_landing_view` and one
   `signup_or_download_start`, with no URL/path/query or typed content.
3. Create a workspace, connect a runtime, create one agent, assign an issue,
   wait for its task to start, and move it to `in_review`. Assign a second
   distinct issue. Retries of the first issue must not increment the issue
   milestones.
4. Run `ListLaunchFunnelEvents` with an empty exclusion list and confirm one row
   for each durable event. Run it again with the test workspace UUID excluded
   and confirm zero rows.
5. Confirm source/campaign are `validation`/`har14`, provider/runtime match the
   selected runtime, timestamps are ordered, and no raw error text appears.
6. Confirm the dashboard conversion, latency, activation, retention,
   reliability, and terminal-failure panels match the test sequence. Remove
   the test PostHog events after validation if project policy permits; do not
   alter operational production rows.

## Known blind spots

- Website events and durable milestones live in separate systems. First-touch
  attribution bridges them after signup, but anonymous users who clear cookies
  or switch devices cannot be joined reliably.
- Durable write paths did not historically persist `X-Client-Platform`, so
  post-signup milestone platform is `unknown`. Adding platform to every write
  path requires a separate schema/API contract and client rollout.
- Existing users created before migration 265 have no first-touch campaign.
- Self-hosted instances with analytics disabled do not emit website events;
  their operational event stream remains locally queryable.
- `issue_in_review` depends on `activity_log`. Legacy transitions predating the
  activity record are not reconstructed from current issue status.

## Product/Data sign-off checklist

- Approve the 3-second qualified-visit definition and per-session deduplication.
- Approve the first-task and first-`in_review` activation semantics.
- Approve the 7-day activation and 14-day retention windows and denominators.
- Approve the sanitized attribution fields and retention policy.
- Supply and maintain the excluded test-workspace list.
- Accept the platform/self-host/cookie blind spots or schedule follow-up work.
- Validate dashboard results against the test-account procedure before launch.
