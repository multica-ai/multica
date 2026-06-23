# Codex Usage Cache Backfill

This runbook describes the one-time hosted data repair for Codex usage rows
created before cached input was normalized at ingestion time.

Do not run this as an automatic database migration. The write step needs an
operator-selected cutoff, dry-run review, and an explicit execute command.

## When To Run

Run this only after the backend image containing `backfill_codex_usage_cache`
has been deployed, and only for databases that need historical Codex usage
correction.

Use the actual hosted deployment time of PR #4083 as `--cutoff`. Do not use the
PR merge time unless it is also the real production cutover time.

## Execution Model

Run the command from the released backend image as a one-time operator job, such
as a Kubernetes Job with the normal backend database secret and network access.
Override the command to execute `./backfill_codex_usage_cache`.

The command defaults to dry-run. It mutates data only when `--execute` is passed.

## Dry Run

First run:

```bash
./backfill_codex_usage_cache --cutoff <RFC3339_DEPLOY_TIME>
```

Optionally limit scope while validating:

```bash
./backfill_codex_usage_cache --cutoff <RFC3339_DEPLOY_TIME> --workspace-id <workspace-uuid>
```

Review the per-workspace/date output:

- `rows`
- `input_before`
- `input_after`
- `input_tokens_removed`
- `clamped_rows`

Proceed only if the totals match the expected overcount shape.

## Execute

After dry-run review:

```bash
./backfill_codex_usage_cache --cutoff <RFC3339_DEPLOY_TIME> --execute
```

For large datasets, throttle writes:

```bash
./backfill_codex_usage_cache \
  --cutoff <RFC3339_DEPLOY_TIME> \
  --execute \
  --batch-size 500 \
  --sleep-between-batches 1s
```

By default, execution rebuilds affected hourly rollups by calling
`rollup_task_usage_hourly_window(...)` for the database update window. Leave
`--rebuild-rollup=true` unless an operator intentionally plans a separate rollup
rebuild.

## Verification

After execution, run the dry-run command again with the same cutoff and scope.
Eligible rows should be zero.

Then verify Usage / Runtime dashboard periods that were previously inflated.

## Safety Boundaries

The command updates only rows that match all of these conditions:

- `provider = 'codex'`
- `cache_read_tokens > 0`
- `input_tokens > 0`
- `COALESCE(updated_at, created_at) < --cutoff`
- optional `--workspace-id` match

Rows without persisted `cache_read_tokens` are intentionally ignored because the
current database cannot accurately reconstruct cached input for them.
