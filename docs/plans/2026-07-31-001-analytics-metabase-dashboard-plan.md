# Multica Product Analytics — Metabase Dashboard Design

**Status**: Draft
**Date**: 2026-07-31
**Context**: Design for a Metabase-based product analytics dashboard reading
from a Postgres read replica of the operational DB. See `docs/analytics.md`
for the existing instrumentation this plan builds on (event taxonomy,
PostHog-retirement history, `issue.first_executed_at` as the core activation
signal).

## Decisions made

- **Target app**: Metabase (BI tool), not a custom app or new service.
- **Data access**: direct read-only connection to a Postgres **read replica**
  of the primary DB — not a new analytics API, not a scheduled export/warehouse.
- Rationale: `docs/analytics.md` already establishes Postgres (not PostHog) as
  the source of truth for product analytics since MUL-4127; this plan is the
  BI layer on top of that existing decision, not a new pipeline.

---

## 0. Setup (before any dashboards)

**Read replica**
- Point Metabase at a streaming Postgres read replica, not primary.
  `agent_task_queue`, `issue`, `github_pull_request` are high-write tables in
  the hot path — don't let ad-hoc dashboard queries compete with them for
  locks/IO.
- Create a dedicated least-privilege role for Metabase:

```sql
CREATE ROLE metabase_ro LOGIN PASSWORD '...';
GRANT CONNECT ON DATABASE multica TO metabase_ro;
GRANT USAGE ON SCHEMA public TO metabase_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO metabase_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO metabase_ro;
```

- **PII guardrail**: revoke column access or mask `user.email`,
  `contact_sales_inquiry` free-text fields (`goals`), `feedback.content`, and
  `workspace_invitation.invitee_email` at the DB or Metabase sandboxing level.
  Every query below uses `split_part(email,'@',2)` (domain only) — never
  select raw email in a shared dashboard. Restrict a separate "full email"
  drill-down question to a locked-down Metabase permission group (e.g.
  Support/CS only).
- Set data-refresh scan frequency to hourly (Admin → Databases) — no need for
  real-time given these are funnel/trend metrics, not ops alerting (that's
  Grafana's job).

**Known gap to flag up front**: there's no `is_demo`/test-workspace flag
populated in the DB (`is_demo` exists on the retired analytics events but is
always `false`). Every dashboard below will include internal/test workspaces
unless you maintain an exclusion list. Recommendation: a small Metabase
**Model** (`m_excluded_workspaces`) seeded with known internal workspace
IDs/slug patterns (`%-test`, `@multica.ai` domains, etc.), left-join-excluded
everywhere, rather than hardcoding the filter into every card.

---

## 1. Collection structure

```
Product Analytics/
├── 00 Semantic Layer (Models — not for direct viewing)
├── 01 Acquisition & Activation
├── 02 Team Expansion (Invites)
├── 03 Core Loop Health (Issues & Agent Tasks)
├── 04 Dev Outcomes (PRs & CI)
├── 05 Retention & Usage
├── 06 Automation Adoption (Autopilot)
└── 07 Pre-Billing Funnel (Waitlist / Sales)
```

Permissions: everyone with product-analytics access sees 01–07; only Data/Eng
sees 00 directly (Models are consumed by other questions, not usually opened
raw).

---

## 2. Semantic layer — Metabase Models

Build these once as **Models** (Metabase's saved-SQL-as-virtual-table
feature) so every downstream dashboard question does simple `SELECT ... FROM
{{model}}` instead of re-deriving joins. This is the single biggest thing
that keeps a Metabase instance maintainable as tables evolve — when a column
renames, fix one Model instead of thirty cards.

> Note: column names below come from a codebase exploration pass, not a
> direct read of every migration file. Verify exact names/types (esp.
> `agent_runtime.owner_id`, `issue.creator_id` typing) against
> `server/pkg/db/migrations/` before wiring these up.

**`m_user_funnel`** — one row per user, the backbone of the activation
funnel:

```sql
SELECT
  u.id AS user_id,
  split_part(u.email, '@', 2) AS email_domain,
  u.created_at AS signup_at,
  u.onboarded_at,
  u.onboarding_questionnaire ->> 'role'      AS role,
  u.onboarding_questionnaire ->> 'use_case'  AS use_case,
  u.onboarding_questionnaire ->> 'team_size' AS team_size,
  fw.workspace_id      AS first_workspace_id,
  fw.created_at        AS first_workspace_joined_at,
  fr.created_at         AS first_runtime_connected_at,
  fi.created_at         AS first_issue_created_at,
  fi.first_executed_at  AS first_issue_executed_at,
  (u.cloud_waitlist_email IS NOT NULL) AS joined_cloud_waitlist
FROM "user" u
LEFT JOIN LATERAL (
  SELECT m.workspace_id, m.created_at FROM member m
  WHERE m.user_id = u.id ORDER BY m.created_at ASC LIMIT 1
) fw ON true
LEFT JOIN LATERAL (
  SELECT ar.created_at FROM agent_runtime ar
  WHERE ar.owner_id = u.id ORDER BY ar.created_at ASC LIMIT 1
) fr ON true
LEFT JOIN LATERAL (
  SELECT i.created_at, i.first_executed_at FROM issue i
  WHERE i.creator_type = 'member' AND i.creator_id = u.id
  ORDER BY i.created_at ASC LIMIT 1
) fi ON true
```

**`m_invite_funnel`** — one row per invitation:

```sql
SELECT
  wi.id, wi.workspace_id, wi.inviter_id,
  split_part(wi.invitee_email, '@', 2) AS invitee_email_domain,
  wi.status, wi.created_at AS sent_at,
  CASE WHEN wi.status = 'accepted' THEN wi.updated_at END AS accepted_at,
  CASE WHEN wi.status = 'accepted'
       THEN EXTRACT(EPOCH FROM (wi.updated_at - wi.created_at)) / 86400.0
  END AS days_to_accept
FROM workspace_invitation wi
```

**`m_agent_task_health`** — one row per agent run, with durations
pre-computed:

```sql
SELECT
  t.id, t.agent_id, t.issue_id, t.status, t.originator_source,
  t.failure_reason, t.created_at, t.dispatched_at, t.started_at, t.completed_at,
  a.workspace_id,
  EXTRACT(EPOCH FROM (t.dispatched_at - t.created_at))   AS queue_wait_seconds,
  EXTRACT(EPOCH FROM (t.completed_at - t.started_at))    AS run_seconds
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
```

**`m_issue_pr_cycle`** — issue → linked PR, cycle time:

```sql
SELECT
  i.id AS issue_id, i.workspace_id, i.created_at AS issue_created_at,
  i.first_executed_at,
  pr.id AS pr_id, pr.state, pr.pr_created_at, pr.merged_at, pr.closed_at,
  ipr.close_intent,
  EXTRACT(EPOCH FROM (pr.pr_created_at - i.created_at)) / 3600.0 AS hours_issue_to_pr,
  EXTRACT(EPOCH FROM (pr.merged_at - pr.pr_created_at)) / 3600.0 AS hours_pr_to_merge
FROM issue i
JOIN issue_pull_request ipr ON ipr.issue_id = i.id
JOIN github_pull_request pr ON pr.id = ipr.pull_request_id
```

**`m_client_usage_rollup`** — reuse the query already in
`docs/analytics.md` (DAU/WAU + desktop built-in-runtime state), saved as a
Model rather than copy-pasted into every dashboard card.

---

## 3. Dashboards

### 01 — Acquisition & Activation

The core funnel from `docs/analytics.md`: **signup → onboarded → workspace
joined → runtime connected → first issue → first issue executed.**

| Card | Viz | Source | Filters |
|---|---|---|---|
| Activation funnel (counts + conversion %) | Funnel | `m_user_funnel`, `count(signup_at)`, `count(onboarded_at)`, `count(first_workspace_joined_at)`, `count(first_runtime_connected_at)`, `count(first_issue_created_at)`, `count(first_issue_executed_at)` | Date range on `signup_at`, `role`/`use_case` breakdown |
| Time-to-activation (signup → first_issue_executed) | Line, median/p90 | `m_user_funnel` | Date range, cohort by signup week |
| Completion path breakdown | Bar | `onboarding_completed` isn't in DB directly — derive from `m_user_funnel.onboarded_at IS NOT NULL` × `joined_cloud_waitlist` as a proxy, since `completion_path` itself is metrics-only (Prometheus). **Flag**: exact `completion_path` enum lives only in Prometheus, not Postgres — this card can only approximate it from DB columns; pull the precise breakdown from Grafana and note it as a cross-reference, don't fake precision here. | — |
| Signup → onboarded, daily trend | Line | `m_user_funnel` | Rolling 90d |
| Role / use_case distribution of activated users | Pie/Bar | `m_user_funnel` filtered `first_issue_executed_at IS NOT NULL` | — |

### 02 — Team Expansion (Invites)

| Card | Viz | Source |
|---|---|---|
| Invite funnel: sent → accepted/declined/expired | Funnel/stacked bar | `m_invite_funnel` group by `status` |
| Days-to-accept distribution | Histogram | `m_invite_funnel.days_to_accept` |
| K-factor proxy: invites sent per activated user | Number/trend | join `m_invite_funnel` (by `inviter_id`) to `m_user_funnel` |
| Workspaces by size (solo vs multi-member) over time | Stacked area | `member` grouped by `workspace_id`, bucketed 1 / 2-5 / 6+ |

### 03 — Core Loop Health (Issues & Agent Tasks)

This is the "is the product actually working" dashboard.

```sql
-- Agent task terminal outcome rates, daily
SELECT date_trunc('day', completed_at) AS day,
       status,
       count(*) AS n
FROM m_agent_task_health
WHERE completed_at >= now() - interval '30 days'
GROUP BY 1, 2
ORDER BY 1
```

| Card | Viz |
|---|---|
| Task terminal status mix (completed/failed/cancelled) daily | Stacked bar |
| Failure reason breakdown | Bar, `failure_reason` |
| Queue wait time & run time (p50/p90) | Line, from `m_agent_task_health` |
| `issue_executed` rate (issues with ≥1 successful run / issues created) | Number + trend | `issue.first_executed_at IS NOT NULL` over `issue.created_at` cohort |
| Runs by `originator_source` (direct_human/autopilot/delegation/comment) | Bar | shows how much execution is human-triggered vs automated |
| Agents per workspace, active agent count | Table | `agent` grouped by `workspace_id`, filter `archived_at IS NULL` |

**Reconciliation card** (use verbatim from `docs/analytics.md`):
DB-completed-tasks-per-day vs. the Grafana `multica_agent_task_terminal_total`
counter — not a Metabase card per se, but worth a text card linking to the
Grafana panel so both live side by side.

### 04 — Dev Outcomes (PRs & CI)

| Card | Viz | Source |
|---|---|---|
| Issue → PR → merge cycle time (median/p90, by week) | Line | `m_issue_pr_cycle` |
| PR outcome mix (open/merged/closed unmerged) | Pie | `github_pull_request.state` |
| CI pass rate on agent-authored PRs | Bar | join `github_pull_request_check_suite` |
| PRs per workspace per week (adoption depth) | Bar | `github_pull_request` grouped |

### 05 — Retention & Usage

| Card | Viz |
|---|---|
| DAU/WAU by client_type (web/desktop) | Line, from `m_client_usage_rollup` |
| Desktop built-in-runtime availability state | Stacked bar (`builtin_runtime_available` / `no_builtin_runtime` / `all_offline` / `unknown`) |
| Workspace-level "still active" (has ≥1 agent_task_queue row in last 14d) | Retention curve / cohort table |

### 06 — Automation Adoption (Autopilot)

| Card | Viz |
|---|---|
| Autopilots created over time | Line |
| Autopilot run outcome mix by `trigger_source` | Stacked bar, from `autopilot_run` |
| Workspaces with ≥1 successful autopilot run / total activated workspaces | Number |

### 07 — Pre-Billing Funnel (Waitlist / Sales)

Given there's no plan/billing table yet, this is the whole "commercial
interest" signal:

| Card | Viz |
|---|---|
| Cloud waitlist signups over time | Line, `user.cloud_waitlist_email` |
| Contact-sales submissions by company_size / use_case | Bar, `contact_sales_inquiry` |
| Waitlist → later became active user? (join on email domain, directional only — no hard user linkage guaranteed) | Table |

---

## 4. Filters & parameters

Add these as **Dashboard filters** (not per-card) wired via Metabase
field-filter template tags so one date-range/workspace picker drives every
card on a page:

- Date range → maps to the relevant `*_at` column per card
- Workspace (optional, for drilling into one customer) → `workspace_id`
- Cohort week (signup week) → for activation-funnel cohort analysis

## 5. Alerts (Metabase native, not Grafana)

Set up Slack/email alerts on:

- Activation rate (`first_issue_executed_at` / `signup_at` count) dropping
  below a threshold week-over-week
- Invite-accept rate dropping
- Task failure rate spiking above baseline

## 6. Open gaps to socialize with the team

- No plan/tier/seats column on `workspace` — can't build an
  MRR/expansion-revenue dashboard from DB alone yet.
- `completion_path` (the richest onboarding-exit signal) lives only in
  Prometheus, not Postgres — the Activation dashboard can't fully
  reconstruct it; either add a DB column or accept Grafana as the source for
  that one metric and cross-link.
- No demo/test-workspace flag — needs a manually maintained exclusion Model
  until `is_demo` gets populated.
- No `pr_review` table — review outcomes only exist as a Prometheus counter,
  can't join to a PR row for per-PR review-latency analysis.

## 7. Next steps

- [ ] Validate all column names/types in this plan against
      `server/pkg/db/migrations/` (this plan was drafted from a codebase
      exploration pass, not a line-by-line schema read).
- [x] Stand up Postgres read replica + `metabase_ro` role — see
      `gitops/base/rds-instance.yaml` (reader `ClusterInstance`) and
      `gitops/base/metabase-ro-init-job.yaml` (role provisioning). Tools
      environment only; requires a manual `METABASE_RO_PASSWORD` SSM param
      under `/agentfarm/tools/` before the init job can succeed — see PR
      description for the exact command.
- [ ] Point Metabase's own deployment at `agentfarm-rds-connection-secret`'s
      `reader_endpoint` key + the `metabase_ro` role (not yet built — no
      Metabase Deployment/Service exists in this repo yet).
- [ ] Build `m_excluded_workspaces` exclusion Model.
- [ ] Build Section 2 semantic-layer Models in Metabase.
- [ ] Build dashboard 01 (Acquisition & Activation) first — validate the
      funnel numbers against known internal expectations before building
      the rest.
- [ ] Build dashboards 02–07.
- [ ] Wire up alerts (Section 5).
