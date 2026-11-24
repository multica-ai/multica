# Issue last-activity backfill runbook

Use this command only after migration 360 is applied and every issue-writing
backend has been upgraded to maintain `issue.last_activity_at`.
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

Migration 375 retires the `idx_issue_workspace_last_activity` serving index
because neither the planned recent-issue window nor a first-party
last-activity sort is active. The backfill remains useful as the semantic
activity clock, but an explicit API `sort=last_activity` request uses a scan and
sort until a replacement serving design is introduced.

Do not roll application writers back to a version that predates
`last_activity_at` after the backfill. The nullable column keeps old readers
compatible, but old writers do not maintain the new clock.
