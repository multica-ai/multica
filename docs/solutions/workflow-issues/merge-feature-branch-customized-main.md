---
title: "Merging feature branch into customized local main — conflict resolution and manual migration"
date: 2026-07-13
category: workflow-issues
module: git-workflow
problem_type: workflow_issue
component: merge-workflow
symptoms:
  - "9 merge conflicts when merging feature branch (based on origin/main) into local main (66 customization commits)"
  - "Backend healthcheck returns 503 with migrations: out_of_date after merge"
  - "schema_migrations table has no dirty column — standard INSERT syntax fails"
root_cause: branch_base_divergence
resolution_type: manual_process
severity: low
tags:
  - git
  - merge
  - migration
  - postgresql
  - schema_migrations
  - pm2
  - conflict-resolution
related_components:
  - server/migrations/
  - schema_migrations
---

# Merging feature branch into customized local main — conflict resolution and manual migration

## Problem

When merging a feature branch (based on `origin/main`) into a local `main` branch that has 66 customization commits, the merge produces multiple conflicts because both branches modified the same files. After resolving conflicts and restarting services, the backend healthcheck returns `503` with `migrations: out_of_date` because a new migration (`166_project_dates`) exists on origin/main but hasn't been applied to the local database.

## Symptoms

- `git merge feat/branch` produces 9 conflicting files (package.json exports, component files, test files).
- Backend `/healthz` returns `{"status":"not_ready","checks":{"db":"ok","migrations":"out_of_date"}}`.
- `psql -c "INSERT INTO schema_migrations (version, dirty) VALUES (...)"` fails with `column "dirty" of relation "schema_migrations" does not exist`.
- PM2 `--migrate-only` flag doesn't exist — the server starts normally and tries to bind the port.

## What Didn't Work

1. **`server/bin/server --migrate-only`** — the server binary doesn't support this flag. It starts the full server, which then fails because port 8081 is already in use.

2. **`INSERT INTO schema_migrations (version, dirty) VALUES (...)"` — the `schema_migrations` table uses `version` (text PK) + `applied_at` (timestamp), not `version` + `dirty`. The standard goose/golang-migrate `dirty` column doesn't exist.

3. **Restarting PM2 expecting auto-migration** — the Multica server does NOT auto-apply migrations on startup. It checks migration status for the healthcheck but doesn't run them.

## Solution

### Step 1: Resolve merge conflicts

For files that both branches added/modified, take the feature branch's version for component files (CI-verified) and keep both sides' imports when they're additive:

```bash
# For "both added" files (new components):
git checkout --theirs packages/ui/components/common/actor-mention-chip.tsx ...

# For additive imports (both sides added different imports):
# Edit manually to keep both imports

# Stage resolved files:
git add <resolved-files>
git commit --no-edit
```

### Step 2: Rebuild and restart backend

```bash
cd server && go build -o ./bin/server ./cmd/server
pm2 restart multica-backend
```

### Step 3: Check migration status

```bash
curl -s http://localhost:8081/healthz
# {"status":"not_ready","checks":{"db":"ok","migrations":"out_of_date"}}
```

### Step 4: Apply migration manually

```bash
# Read the migration file
cat server/migrations/166_project_dates.up.sql

# Apply the SQL directly
psql -h localhost -p 5433 -U fengzhao -d multica -f server/migrations/166_project_dates.up.sql

# Record in schema_migrations (note: no 'dirty' column)
psql -h localhost -p 5433 -U fengzhao -d multica -c \
  "INSERT INTO schema_migrations (version) VALUES ('166_project_dates') ON CONFLICT DO NOTHING;"
```

### Step 5: Verify

```bash
curl -s http://localhost:8081/healthz
# {"status":"ok","checks":{"db":"ok","migrations":"ok"}}
```

## Why This Works

- The merge conflicts arise because the feature branch was based on `origin/main` (which has evolved) while local `main` has customization commits on the same files. Both sides made legitimate changes; resolution requires choosing the newer version or combining additive changes.
- The `schema_migrations` table in this project uses `version` (text, PK) + `applied_at` (auto-timestamp) — not the standard goose `dirty` boolean. Manual INSERT with just `version` is sufficient.
- The server does NOT auto-migrate — it only checks. Migrations must be applied manually or via a separate migration runner.

## Prevention

- **Before merging a feature branch into a customized local main**, check `git merge-base` to understand the divergence. If the feature branch is based on `origin/main` and local main has many customization commits, expect conflicts and plan for manual resolution.
- **Always check `/healthz` after restarting the backend** — it catches migration drift immediately.
- **`schema_migrations` table schema varies by project** — don't assume the standard goose `dirty` column. Check `\d schema_migrations` before writing INSERT statements.
- **The server binary doesn't auto-migrate** — this is a design choice, not a bug. Migrations are applied separately.

## Related Issues

- `docs/solutions/workflow-issues/unapplied-migrations-after-upstream-upgrade.md` — covers a similar scenario after upstream version upgrades
- `docs/solutions/workflow-issues/safe-upstream-upgrade-with-local-customizations.md` — covers the broader upgrade workflow for self-hosted instances

## Related Artifacts

- Migration file: `server/migrations/166_project_dates.up.sql` (adds `start_date` and `due_date` columns to `project` table)
- Health check: `server/cmd/server/health.go` — `readinessQuery` checks `schema_migrations` against `migrations.AllVersions()`
