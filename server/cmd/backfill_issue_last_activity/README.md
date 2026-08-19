# Issue last-activity backfill runbook

Use this command only after migrations 360 and 361 are applied and every
issue-writing backend has been upgraded to maintain `issue.last_activity_at`.
Running it while an older writer is still live can make that writer's later
changes invisible to the activity clock.

From `server/`:

```bash
go run ./cmd/backfill_issue_last_activity
```

The command uses bounded transactions, an id keyset watermark, `SKIP LOCKED`,
a delay between batches, and a session advisory lock. It is safe to interrupt
and restart. Use `--batch-size`, `--sleep-between-batches`, and `--max-batches`
to reduce load or run a bounded canary. If consecutive passes make no progress
while rows remain, the command fails after 10 passes instead of repeatedly
counting the table forever. Release the long-held row locks and rerun, or adjust
`--max-stalled-passes`; setting it to 0 explicitly disables this guard.
Completion is explicit: do not depend on complete historical activity ordering
until the command logs `remaining=0`.

Migration 361 builds the serving index before this operator-run backfill so
new application versions can sort safely throughout a rolling deployment. The
tradeoff is index maintenance and possible bloat while historical rows are
updated. After a large backfill, rebuild it online during a normal maintenance
window:

```sql
REINDEX INDEX CONCURRENTLY idx_issue_workspace_last_activity;
```

Do not roll application writers back to a version that predates
`last_activity_at` after the backfill. The nullable column keeps old readers
compatible, but old writers do not maintain the new clock.
