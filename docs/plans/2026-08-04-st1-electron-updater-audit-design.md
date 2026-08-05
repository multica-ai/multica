# ST-1 Electron Updater Audit Design

## Context

The host storage constitution requires a monthly audit of Electron updater
residue after Tencent Meeting left more than 15 GiB of stale update packages.
The existing storage-governance line already owns a cron-to-FDA bridge, a
single-instance lock, atomic JSON evidence, and Lark alert delivery. The audit
must extend that line instead of creating a second garbage-collection owner.

## Considered approaches

1. Extend `retention_worker.py` with a monthly-gated, read-only audit. This
   reuses the existing scheduler, lock, evidence writer, and alert channel while
   keeping updater scans out of the minute-level breaker hot path. This is the
   selected approach.
2. Add a dedicated monthly LaunchAgent and script. This is easy to isolate, but
   duplicates scheduling, locking, logging, and alert ownership.
3. Add updater scans to `storage_guard.py`. This gives fast visibility, but
   recursive size scans would make the low-water breaker slower and less
   predictable.

## Architecture

`retention_worker.py` gains a bounded scanner driven by an
`electron_updater_audit` config object. Each configured glob must resolve below
the configured home directory. Matches are de-duplicated, symlinks are rejected,
and directory traversal never follows symlinks. The scanner records bytes, file
count, newest and oldest mtimes, and age for each match; it never moves or
deletes data.

The formal retention run checks for the current Shanghai calendar month's
evidence before touching the external archive volume. On or after the configured
day of month, a missing report triggers the scan. This ordering lets ST-1 leave
evidence even if the external archive is unavailable. A manual audit-only CLI
mode forces the same scanner for commissioning and incident response without
claiming cron lineage.

The audit writes
`~/.org/metrics/st1-electron-updater-audit-YYYY-MM.json` atomically. `green`
means the scan completed and no configured size or stale-residue threshold was
crossed; `attention` means operator review is warranted; `red` means the scan
could not provide complete evidence. Formal `attention` and `red` results use
the existing alert channel. A `red` result also fails the retention invocation
closed, while `attention` does not block unrelated canary or GC reporting.

## Initial scan surface

The host config covers the known sharp edges without a full-disk search:

- sandboxed `UpdatePackages` roots (Tencent Meeting pattern);
- `update.noindex`, `update_downloading`, and `Software Update` directories;
- Electron `*-updater` and scoped updater caches;
- Squirrel `*.ShipIt` caches;
- MiniMax Agent `hot-update` payloads;
- pending/update ZIP files under Multica daemon state.

Thresholds are configuration, initially 5 GiB total, 1 GiB per candidate, and
45 days stale for candidates of at least 100 MiB. The audit is observational;
crossing a threshold does not authorize cleanup.

## Verification

Unit tests use temporary directory trees to prove discovery, de-duplication,
byte counts, no symlink following, threshold classification, month/day gating,
and forced audit behavior. Commissioning runs the audit-only CLI against the
real host configuration, validates the JSON schema and current-month mtime, and
then verifies the deployed worker and config match the reviewed repository
files.
