---
title: "Run database migrations after every upstream upgrade — and watch for migration version reshuffles"
date: 2026-07-10
category: "workflow-issues"
module: "database/migrations"
problem_type: "workflow_issue"
component: "development_workflow"
severity: "medium"
applies_when:
  - "Self-hosted Multica upgraded by merging upstream into local main"
  - "A feature silently degrades after upgrade: one API returns 500 while a sibling endpoint still works"
  - "A sqlc-generated query references a column that does not exist in the local database"
  - "Upstream reshuffled migration file numbers (e.g. chat migrations renumbered 145-148 → 151-156)"
  - "make migrate-up / go run ./cmd/migrate up errors out partway and leaves later migrations unapplied"
tags: [upstream-upgrade, database-migrations, sqlc, migrate, self-hosted, schema-drift, version-reshuffle, api-500]
---

# Run database migrations after every upstream upgrade — and watch for migration version reshuffles

## Context

After merging upstream v0.3.42 (`2d34a8d0b`) into a locally customized self-hosted instance, the issue detail page's right sidebar silently regressed: the "执行日志" (Execution log) section that lists every agent run disappeared, leaving only the "运行次数" (Runs) counter showing a number (6). It looked like a UI regression. It was not.

The upgrade shipped new database migrations that were never applied to the local database, so a sqlc-generated query referenced a column that did not exist and the endpoint backing the detail list returned HTTP 500.

### Symptom split — two endpoints, two tables

The confusion came from two independent data sources that read as one feature:

- **"运行次数" counter** — `GET /api/issues/:id/usage` → `GetIssueUsageSummary`: `COUNT(DISTINCT task_id) FROM task_usage JOIN agent_task_queue`. Returned 200 with `task_count: 6`.
- **per-run detail list** — `GET /api/issues/:id/task-runs` → `ListTasksByIssue`: `SELECT ... coalesced_comment_ids, delivered_comment_ids FROM agent_task_queue`. Returned **HTTP 500 "failed to list tasks"**.

The frontend (`ExecutionLogSection`) hides itself entirely (`return null`) when its task list is empty, so a 500 (→ empty `useQuery` data) made the whole section vanish, while the usage counter — backed by a different table — kept showing 6.

### Root cause

Reproducing the failing query against the database directly surfaced the real error:

```
ERROR: column "coalesced_comment_ids" does not exist
```

`coalesced_comment_ids` and `delivered_comment_ids` are added by migrations `150_agent_task_coalesced_comments` and `157_agent_task_delivered_comments`, both introduced by v0.3.42. Comparing `schema_migrations` against the migration files showed **9 unapplied migrations** (149–157).

### Why `migrate up` had not fixed it — version reshuffle

Upstream renumbered the chat migrations during v0.3.42: the previously-applied `145_chat_read_cursor` / `146_chat_pinned_agent` / `147_chat_agent_intro` / `148_chat_session_pinned` became `151` / `152` / `154` / `155`, with new migrations `149/150/153/156/157` inserted. Migration `151_chat_read_cursor` contains:

```sql
ALTER TABLE chat_session ADD COLUMN last_read_at TIMESTAMPTZ NOT NULL DEFAULT now();
```

— **without `IF NOT EXISTS`**. Because the local DB had already applied the old `145_chat_read_cursor` (which created `last_read_at`), `migrate up` errored on `151` and aborted the whole run, leaving `150`/`157` (the column adds) unapplied. The migration runner applies each `.up.sql` via one `conn.Exec` and stops at the first error (`server/cmd/migrate/main.go:274`).

## Guidance

### 1. After every upstream merge, run migrations explicitly

The upgrade workflow in `safe-upstream-upgrade-with-local-customizations.md` verifies `go build` / `typecheck` / `test` / `build` but does **not** run migrations. Add it to the checklist:

```bash
cd server && DATABASE_URL="postgres://fengzhao@localhost:5433/multica?sslmode=disable" go run ./cmd/migrate up
```

`cmd/migrate` reads `DATABASE_URL` and falls back to `postgres://multica:multica@localhost:5432/multica` (`server/cmd/migrate/main.go:120`). On a self-hosted instance whose DB is on a non-default port/user, **you must export `DATABASE_URL`** or the tool connects to the wrong (or nonexistent) database and "migrations ran fine" masks that nothing happened.

### 2. Detect unapplied migrations

```bash
DB="postgres://fengzhao@localhost:5433/multica?sslmode=disable"
comm -23 \
  <(ls server/migrations/*.up.sql | xargs -n1 basename | sed 's/\.up.sql$//' | sort -u) \
  <(psql -t -A "$DB" -c "SELECT version FROM schema_migrations" | sort -u)
# → migrations present in files but absent from the applied-records table
```

Any output is schema drift. The inverse (`comm -13`) lists applied versions whose files no longer exist — a sign of a version reshuffle and a leading indicator that a non-idempotent collision is coming.

### 3. Diagnose a 500 by reproducing the failing query

Backend handlers wrap query errors as generic 500s. Reproduce the exact SQL to see the real cause — grab the query from the sqlc-generated source (`server/pkg/db/generated/*.sql.go`) and run it with a real id:

```bash
psql "$DB" -c "SELECT ... coalesced_comment_ids, delivered_comment_ids FROM agent_task_queue WHERE issue_id = '<id>'::uuid;"
# ERROR: column "coalesced_comment_ids" does not exist  ← real cause
```

### 4. When `migrate up` aborts on a version reshuffle, reconcile the conflicting migration

Don't blindly re-run. Identify the migration whose non-idempotent statement collides with an already-applied (renumbered) predecessor, apply its intent idempotently, and record it so the runner skips it:

```sql
-- 151_chat_read_cursor: last_read_at already exists from old 145;
-- only unread_since is missing. Apply the intent idempotently + record it.
BEGIN;
  ALTER TABLE chat_session ADD COLUMN IF NOT EXISTS unread_since TIMESTAMPTZ;
  UPDATE chat_session SET last_read_at = unread_since - interval '1 microsecond'
   WHERE unread_since IS NOT NULL;
  INSERT INTO schema_migrations (version) VALUES ('151_chat_read_cursor');
COMMIT;
```

Then `migrate up` runs clean: it skips the reconciled migration and applies the rest — the truly-new column adds plus the idempotent `IF NOT EXISTS` ones.

### 5. Verify the user-visible symptom, not just the schema

```bash
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOK" \
  -H "X-Workspace-ID: $WS" "http://localhost:8081/api/issues/$ISSUE/task-runs"
# Before: 500   After: 200
```

Schema applied ≠ feature works. The endpoint that was 500'ing must return 200.

## Why This Matters

- **Schema drift is silent until a query hits the missing object.** The server still booted, most pages worked, and the usage counter showed a number — only the one query touching the new column failed. A 500 on a single endpoint reads like a code bug, not a missed migration.
- **The two-table symptom split is misleading.** "Counter works, detail list gone" looks like a UI regression. It is not — the counter and the list are backed by different tables (`task_usage` vs `agent_task_queue`), so one can succeed while the other fails. Always check the network response, not just the rendered UI.
- **Version reshuffles turn a one-command fix into a trap.** `migrate up` is normally idempotent and self-healing — but only if every migration is itself idempotent. A single missing `IF NOT EXISTS` on a reshuffled migration aborts the entire run and blocks every migration after it, including unrelated column adds. After an upgrade that touched migration numbering, always confirm `migrate up` reaches `Done.` with zero unapplied migrations remaining.
- **The migrator's DB default is wrong for self-host.** It assumes `localhost:5432/multica:multica`. A self-hosted instance on `:5433` silently targets a different database if `DATABASE_URL` isn't set.

## When to Apply

- After any upstream merge into a self-hosted instance — run `migrate up` and confirm it finishes, regardless of whether the merge touched `server/migrations/`.
- When a single endpoint returns 500 after an upgrade while sibling endpoints work — suspect an unapplied migration adding a column/constraint the query references.
- When `comm` between migration files and `schema_migrations` is non-empty — close the gap before relying on the app.
- When upstream renumbered migrations (applied versions exist whose files are gone) — expect a non-idempotent collision and reconcile before running `migrate up`.

## Examples

### Full reconciliation of the v0.3.42 drift (149–157)

```bash
DB="postgres://fengzhao@localhost:5433/multica?sslmode=disable"

# 1. Reconcile the one conflicting migration (151) idempotently + record it
psql "$DB" -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;
  ALTER TABLE chat_session ADD COLUMN IF NOT EXISTS unread_since TIMESTAMPTZ;
  UPDATE chat_session SET last_read_at = unread_since - interval '1 microsecond'
   WHERE unread_since IS NOT NULL;
  INSERT INTO schema_migrations (version) VALUES ('151_chat_read_cursor');
COMMIT;
SQL

# 2. Run the standard migrator — applies 149/150/157 (real schema changes),
#    idempotently no-ops 152-156 (objects already exist under the old numbering),
#    and skips 151 (just recorded)
cd server && DATABASE_URL="$DB" go run ./cmd/migrate up
#   up  149_issue_origin_agent_create
#   up  150_agent_task_coalesced_comments
#   skip 151_chat_read_cursor (already applied)
#   up  152 ... 156   (idempotent)
#   up  157_agent_task_delivered_comments
#   Done.

# 3. Verify schema_migrations now lists 149-157; total went 194 → 203
psql -t -A "$DB" -c "SELECT count(*) FROM schema_migrations;"   # 203

# 4. Verify the 500 is gone
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOK" \
  -H "X-Workspace-ID: $WS" "http://localhost:8081/api/issues/$ISSUE/task-runs"  # 200
```

### Spotting the version reshuffle before it bites

```bash
# Applied versions whose files no longer exist (= upstream renamed them):
comm -13 \
  <(ls server/migrations/*.up.sql | xargs -n1 basename | sed 's/\.up.sql$//' | sort -u) \
  <(psql -t -A "$DB" -c "SELECT version FROM schema_migrations" | sort -u)
# 145_chat_read_cursor
# 146_chat_pinned_agent
# 147_chat_agent_intro
# 148_chat_session_pinned
# → these now live under 151/152/154/155; expect a collision there
```

## Related

- `docs/solutions/workflow-issues/safe-upstream-upgrade-with-local-customizations.md` — the upgrade workflow this gap lives in; its verification step omits running migrations (Guidance §1).
- `docs/solutions/workflow-issues/pnpm-install-after-upstream-merge.md` — sibling "post-merge step the workflow forgot" lesson: a file-level merge does not run installers, and the same is true of migrations.
- Merge `2d34a8d0b` (v0.3.42 upgrade); migrations `150_agent_task_coalesced_comments`, `157_agent_task_delivered_comments`; query `ListTasksByIssue` in `server/pkg/db/generated/agent.sql.go`.
