# Decision: Product analytics live in the database and Prometheus, not PostHog

Status: implemented

## Problem

PostHog had become a second, largely unused copy of data already queryable from the operational database. Every server-side product event was emitted twice — once as a Prometheus counter and once as a PostHog event — while the rows those events described sat in Postgres the whole time. The frontend carried its own funnel instrumentation on top: page views, download-intent steps, an onboarding mirror of a server event, runtime path and detection events, feedback-open events, and the attribution backfill modal's four events.

Two copies of the same signal do not agree for long, and when they disagree it is not obvious which is wrong. Meanwhile nobody was reading the PostHog dashboards, so the drift was invisible and the instrumentation was pure carrying cost.

## Decision

Product analytics have two homes and PostHog is not one of them.

**The operational database is the source of truth.** Every product question about what happened — signups, workspaces, issues, tasks, agents — is answered by querying the rows.

**Prometheus and Grafana carry the counters.** Server-side events are flagged by `analytics.IsMetricsOnly`, so `metrics.RecordEvent` increments the Grafana counter and ships nothing onward. The `analytics.*` event constructors survive only to drive those counters. The runtime lifecycle, autopilot run lifecycle, and agent task events were already Prometheus-only.

**Frontend funnel instrumentation is deleted**, not disabled.

PostHog keeps exactly one job, from the frontend only: error and crash monitoring that has no database equivalent — `$exception` autocapture with redaction and de-duplication in `before_send`, plus the desktop stability events `client_crash` and `client_unresponsive`. Identity calls are retained only to attach those.

The `multica_signup_source` attribution cookie stays. It is independent of page-view tracking and feeds the `signup_source` Prometheus label. Persisting the raw source channel and country to the database — the one signal PostHog uniquely held — is separate work.

Task success reconciles database against Prometheus: compare completed rows in `agent_task_queue` against the corresponding `multica_*` counter in Grafana, where sustained drift means a missing emission site or an unhealthy metrics pipeline. `issue_executed` remains the product-level success signal, reconciled against `issue.first_executed_at`.

## Alternatives considered

**Keep PostHog and fix the drift.** Rejected. Reconciling two pipelines is ongoing work that buys nothing the database does not already answer, and the dashboards that would have justified it were not being used.

**Keep PostHog for the frontend funnel only, dropping server events.** Rejected. The funnel events were the least-read instrumentation and the most likely to rot, since they encode UI structure that changes faster than the data model. What the frontend uniquely sees — exceptions and crashes — has no database equivalent, and that is exactly what was kept.

**Move error monitoring off PostHog too.** Rejected as unnecessary scope. Crash and exception capture is the one thing PostHog does that the operational database structurally cannot, and it works.

## Consequences

There is one source of truth per product question, so a disagreement between two systems is no longer a category of bug.

Ad-hoc product analysis is SQL against the operational database rather than a query builder. That is a real ergonomic loss for anyone who was using the PostHog UI, accepted because the numbers it produced could not be trusted against the database anyway.

Cohort and funnel analysis over historical PostHog data is gone with the dashboards.

Raw acquisition source channel and country are not persisted anywhere until the separate database work lands; only the `signup_source` label survives.

Local development and self-hosted instances run with an empty `POSTHOG_API_KEY`, so nothing leaves the process unless an operator opts in.
