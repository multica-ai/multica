# Storage governance runbook

This directory contains two deliberately small host-safety jobs:

- `storage_guard.py` samples the host once per minute and applies configured
  low-water admission controls. It never archives or deletes data.
- `retention_worker.py` is the single owner of external-volume canaries,
  workspace GC eligibility, transactional archives, and the monthly ST-1
  Electron updater residue audit.

## Safe rollout

Start the retention worker with both `archive_enabled` and `delete_source` set
to `false`. In this mode every formal cron invocation checks the exact external
volume UUID, verifies a nested canary tree by file count, byte count, entry
metadata, and deterministic sample hashes, and writes only a GC dry-run report.

GC eligibility is fail-closed. A workspace is listed as eligible only when its
issue is `done` or `cancelled`, its matching run is terminal, the configured
seven-day retention window has elapsed, children are terminal, no run/lease is
active, no pin or open file exists, no recent write exists, and local identity
agrees with the control plane. Filesystem traversal never follows symlinks;
out-of-tree symlinks reject the candidate.

After an operator approves a dry-run list, its one-time `approval_token` values
must be put in `approved_candidates` before `archive_enabled` is enabled; keep
`delete_source` false. A completed archive marker consumes the token so a later
cron run cannot archive the same snapshot again. A transaction freezes the source manifest,
copies to `.partial`, fsyncs and verifies it, atomically renames the archive,
and writes `COMPLETE.json`, then re-hashes the committed payload. Automated
source deletion is deliberately rejected until Multica exposes a producer-shared
lease; filesystem isolation alone cannot close the open-file-descriptor race.

The guard's minute path samples free space, swap, and daemon state before any
recursive work. Directory/category scans run from a 15-minute cache after the
breaker decision, so capacity attribution cannot delay low-water enforcement.

Admission control is grouped by `daemon_id`, not by the first profile found.
The guard discovers every live local `multica daemon start` process, resolves
its profile-specific health endpoint, applies the same owner claim to every
instance with the selected ID, and then re-reads every PID. An unqueryable or
changing group fails the enforcement check closed; a different daemon ID is
never paused by that claim.

`retention_worker.py --workspace-stale-dry-run` is an observation-only scan of
task directories whose directory mtime is older than one day. It writes paths
and byte counts to `workspace_stale_report_path` with deletion explicitly
unauthorized. The guard reports that amount as
`workspace_gc_eligible_bytes` for capacity planning, while
`workspace_gc_deletion_eligible_bytes` remains the stricter control-plane and
lease-gated value. The stale signal never supplies an archive approval token.

## ST-1 Electron updater audit

The retention worker also owns the read-only monthly audit for Electron updater
residue. On or after `electron_updater_audit.day_of_month`, it scans only the
configured home-relative globs and atomically writes
`st1-electron-updater-audit-YYYY-MM.json` below the configured report directory.
It does this before external-volume checks, so an unavailable archive disk does
not erase the month's ST-1 evidence.

Matches are de-duplicated and recursively sized without following symlinks.
The report records file counts, bytes, mtimes, stale candidates, and threshold
reasons. It never moves or deletes a match. `attention` sends the existing Lark
alert once for that month's report; an incomplete `red` scan fails the formal
worker closed. A valid `green` or `attention` report suppresses later scans in
the same Shanghai calendar month.

Commission the exact same scanner without cron lineage or the external archive:

```bash
/usr/bin/python3 /Users/example/.local/libexec/storage-governance/retention_worker.py \
  --config /Users/example/.local/libexec/storage-governance/retention-config.json \
  --electron-audit-only
```

Audit-only mode still takes the retention worker's single-instance lock and
forces a fresh report. It is for commissioning and incident response; scheduled
evidence continues to come from the existing formal cron owner.

## Formal cron lineage

Use the same command for canary, GC audit, and archive. The environment marker
prevents a normal manual invocation from being mistaken for a cron result:

On macOS, `/usr/sbin/cron` normally lacks Full Disk Access to external media.
The formal entry therefore runs a synchronous bridge. The bridge proves its
own live `cron` ancestry, writes a fresh one-time token, starts the FDA-capable
LaunchAgent, and waits for a token-matched receipt. The worker refuses a green
result unless that bridge process and its cron parent are still alive:

```cron
*/15 * * * * /usr/bin/python3 /Users/example/.local/libexec/storage-governance/retention_cron_bridge.py --trigger /Users/example/.local/state/storage-governance/cron-trigger.json --receipt /Users/example/.local/state/storage-governance/cron-receipt.json --alert-log /Users/example/.local/state/storage-governance/retention-alerts.jsonl --config /Users/example/.local/libexec/storage-governance/retention-config.json
```

Keep the lock and report on the internal volume, and the archive root on the
external volume. A lock collision or canary failure exits nonzero and records
an alert; it never starts a second copy or removes a source.
