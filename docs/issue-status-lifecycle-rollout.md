# Four-category issue lifecycle (MUL-7240)

## Contract

| Stored category | Fixed built-in statuses | Custom status behavior |
| --- | --- | --- |
| `unstarted` | `backlog`, `todo` | Open work; no Backlog parking or promotion rule |
| `started` | `in_progress`, `in_review`, `blocked` | Open work; no review completion, blocked failure, or active-status recovery rule |
| `done` | `done` | Successful terminal lifecycle |
| `closed` | `cancelled` | Cancelled terminal lifecycle |

Done is not an intermediate step toward Closed. In Review and Awaiting Response
belong to Started. A status's label does not grant behavior. Built-ins remain
locked, including name, color, category, archival and deletion. Custom
keys and categories remain immutable; their labels, descriptions and colors may
be edited, and all active statuses can be reordered within their category.
Custom statuses can only be archived after every referencing issue has been
moved elsewhere, including completed/canceled issues. The archive endpoint
checks the count under the same catalog lock used by status writers and returns
409 with `code: issue_status_in_use` and `issue_count` when occupied. Older
clients display the accompanying error message; no new request field is required.
During mixed-server rollout, an old server can still accept the previous
archive behavior until it is replaced. Deploy the backend before relying on
the restriction; no schema migration or automatic issue migration is involved.

Archived statuses no longer create default board/list/swimlane columns. Old
archives with historical issues remain resolvable and can be inspected/moved
out via Settings > Show archived > View issues (an independent, transient exact-status
list). This includes sub-issues and terminal issues without changing saved-view
selection, workspace filters, or All/Members/Agents preferences. Moving issues
does not automatically archive the status; retry archive explicitly once empty.
The archive itself neither moves issues nor emits issue transition events.

The server's `Effective`/SQL `issue_effective_status` functions preserve built-in
identity, map custom Done/Closed to terminal behavior, and leave other custom
keys distinct. Lifecycle grouping uses `Category`, not the effective behavior.
Ordinary creation, assignment, comments, and explicit run requests retain their
existing trigger rules. Merely entering a custom Started status is not an
instruction to start, finish or fail an agent. The built-in platform skill and
new daemon task briefs describe this distinction. This is not a workflow engine;
PR #7990 is subsequent work, not part of this release.

## Migration and release (MUL-7365)

The supported upgrade entrypoint is the **matching revision's migration runner**
(`cd server && go run ./cmd/migrate up`, with the approved target DATABASE_URL).
Do not apply pending 469 directly with psql or use an older migration binary:
469 contains the historical unbounded rewrite and is intentionally unchanged.
Production deployment and database execution require a separate operator handoff.

Before execution, collect a fresh read-only snapshot on the approved target:

```sql
SELECT version FROM schema_migrations
WHERE version LIKE '469_%' OR version LIKE '477_%' OR version LIKE '478_%';
SELECT category, is_system, archived_at IS NOT NULL AS archived, count(*)
FROM issue_status GROUP BY 1, 2, 3 ORDER BY 1, 2, 3;
SELECT conname, convalidated, pg_get_constraintdef(oid)
FROM pg_constraint WHERE conrelid = 'issue_status'::regclass;
```

The September 14 snapshot of 591,052 legacy rows is historical, not proof of the
current target's state. Compare actual constraints/data as well as the ledger:
a skipped migration is recorded, so a 469 ledger row alone does not prove a backfill.

| Starting environment | Runner behavior | Phase-1 result |
| --- | --- | --- |
| 469 has not run | Record 469 as superseded without executing its SQL; continue through 478 | Old rows remain; old/new constraints and readers coexist |
| 469 already committed | Preserve its ledger and SQL; apply additive 478 | New rows remain; the same compatibility schema is installed |
| 478 failed waiting for a lock | No 478 ledger entry; retry after contention is resolved | Its transaction rolls back; committed earlier migration entries remain |
| 478 committed but acknowledgement was lost | Resume runner; replay is safe if its ledger write was lost | No row rewrites or duplicate objects |

478 replaces only constraints and functions. The entire file is one implicit
transaction under pgx's multi-statement Exec. `SET LOCAL lock_timeout = '2s'` and
`statement_timeout = '10s'` bound this step; they do not leak to following files.
Both expanded CHECKs use NOT VALID, avoiding a validation scan under the
ACCESS EXCLUSIVE lock while still checking every subsequent write. There is no
unconstrained commit window. The constraints remain unvalidated in phase 1.
A deployment is not ready if this migration fails; diagnose contention and retry
the same runner rather than deleting ledger rows or bypassing readiness.

Release sequence:

1. Phase 1 is based on main, including the GC/recovery fix merged in PR #8405
   (MUL-7364). It builds on that GC lifecycle protocol and adds the legacy
   `cancelled` spelling to its recovery prefilter. Both are required before
   permitting data conversion.
2. Apply the matching runner through 478, then finish the agreed full backend
   replacement. Mixed storage is supported; long-lived old backend instances
   and application rollback are not part of the agreed fix-forward policy.
3. Verify old/mixed/new catalog reads, custom terminal resolution, archived
   references, create/update/reorder, old and new grouped requests, claim, both
   GC endpoints and delegated-failure recovery. No backfill needs to run for
   this release to work. New catalog inserts use four values; metadata updates,
   archive and reorder normalize touched legacy rows. All issue keys, identities,
   and custom lifecycle behavior remain unchanged by storage normalization.
4. Update clients/daemons for the new feature. Old clients remain supported by
   the wire adapters below. Old daemon prompts can still describe obsolete
   custom-status automation inheritance; upgrading the daemon is required to
   replace those instructions. Do not automatically restart or replay tasks.

**Phase 2 is an explicitly driven internal API**, documented in
[the maintenance jobs runbook](maintenance-jobs.md). It uses a shared
maintenance_job record, one indexed ID page per request, and atomic
data/checkpoint commits. It supports dry-run/progress/retry/pause and includes
system, custom and archived catalog rows. Before starting, prove
all writers use the new format and the compatible backend is fully deployed.
Set workload limits from the environment's API latency, lock wait, I/O, WAL and
replication budgets. Pausing the job must leave normal reads/writes working.
Do not treat a skipped or failed batch as completion.

**Phase 3 uses the normal upgrade entrypoint for both SaaS and self-host.**
There is no new environment flag, automatic maintenance worker, or manual job
requirement for self-host. The matching container entrypoint runs migrations
before starting the API, as before. Do not edit/replay historical 469 or delete
its ledger entry: it may have executed or been recorded as skipped.

| Migration | Work and transaction boundary |
| --- | --- |
| 491 | UPDATE only the six legacy spellings, including system/custom/archived rows; preserve every other field. Commit data independently of DDL. |
| 492 | Replace both category and canonical-system CHECKs atomically, NOT VALID, with a 2s lock timeout and 10s statement timeout. |
| 493 | Validate both CHECKs in a separate transaction under SHARE UPDATE EXCLUSIVE, allowing ordinary reads/writes. |
| 494 | Retire the old stored `cancelled` branch of `issue_effective_status`; built-in `cancelled` identity remains. |

Application catalog/recovery queries now read the four stored values directly.
The normalization function remains for historical migration replay and job code;
installed-client adapters and accepted category aliases remain supported.

For **SaaS**, deploy the compatible PR1/PR2 revision first, replace all old
writers, complete an **apply** job and its verification, and retain its audit
JSON. Before releasing PR3, check the actual target database again:

```sql
-- Must return 0. Do not infer this from migration 469's ledger entry.
SELECT count(*) AS remaining_legacy FROM issue_status
WHERE category IN ('backlog','todo','in_progress','in_review','blocked','cancelled');
-- Must return 0.
SELECT count(*) AS invalid_catalog_rows FROM issue_status
WHERE category NOT IN ('unstarted','started','done','closed')
   OR (is_system AND (key,category) NOT IN (
     ('backlog','unstarted'),('todo','unstarted'),('in_progress','started'),
     ('in_review','started'),('blocked','started'),('done','done'),('cancelled','closed')));
```

Block SaaS release until both counts are zero, old writers are gone, the apply
job is completed, and no apply work remains active. This is an operator/release
prerequisite, not a self-host startup setting; this repository does not establish
which external SaaS CD pipeline enforces it. Migration 491 then updates zero
rows, although the predicate and constraint validation can still scan the table.
Schedule validation against the target's I/O budget. Never release this revision
to a large unconverted SaaS database: startup would perform the bulk UPDATE.

For **self-host**, upgrade normally, including when skipping PR1/PR2 releases.
491 converts the local catalog automatically, then 492–494 converge the schema.
A fresh install follows the same path. The accepted small-database tradeoff is
an UPDATE/validation startup window; this is not a zero-downtime guarantee for
large self-host installations. The default Helm startup probe budget is 10
minutes. An operator with unusually large data can use the SaaS staged path.

If 492 times out, 491 stays committed and retry resumes at 492. If 493 fails,
strict checks already protect new writes and readiness remains failed. Identify
and stop the stale writer, repair residual values forward, and rerun the same
runner without deleting ledger rows. All new SQL files tolerate replay after
an uncertain ledger acknowledgement; there is no data+DDL mega-transaction.
After success, verify both CHECKs have `convalidated=true`, residual counts are
zero, and API readiness passes. Keep job history; phase 3 never starts a job.
The category v1 job intentionally fails preflight after contract because it
requires the expanded schema. Existing job records remain readable.

Release policy is fix-forward. 478's down file preserves the compatible schema;
469 already refuses reversal. Do not roll back to code that only understands one
storage vocabulary. If any later stage fails, keep compatibility active, repair
forward and resume that stage. Do not remove client wire adapters with storage
cleanup: no minimum supported client version or retirement date has been agreed.

Migration versions 469/470 were previously renumbered during development.
Main's two historical 468 filenames retain their existing lint exception; 478
adds no collision. The runner ledger uses the full filename stem. Do not rewrite
or delete existing entries, including earlier preview-version entries.

## Installed-client compatibility

Status keys on issue create/get/update/assign do not change. The existing
catalog `category` / `categories` and issue `status_category` response fields
retain the seven-value wire enum accepted by installed clients. Built-ins emit
their fixed key; custom statuses emit `todo`, `in_progress`, `done`, or
`cancelled` as a lifecycle encoding. New clients normalize those fields into
four categories. This is one API-boundary adapter, not a second stored model
or behavior mapping. Old category inputs are accepted on catalog creation,
reordering and category filters, and normalized to the new lifecycle.

The retained `group.kind: status_category` and compound
`secondary: status_category` protocols default to seven-value **wire buckets**.
Built-ins keep their own bucket; custom statuses use the same bucket as their
per-row wire category. Headers, counts, secondary_values and row pagination share
that mapping, including archived statuses. A category grouping
request can explicitly set `category_format: lifecycle` to use four buckets;
other values or use with a non-category group are rejected. Both group and row
cursors bind the category format; stale or cross-format cursors return the
existing 409 cursor_query_mismatch response and must restart from the first page.
These changes restore the old consumer's whitelist contract without modifying
installed clients. The general list API's legacy category-filter aliases still
normalize to lifecycle (and can broaden as categories combine), as established
in MUL-7240. Exact status filters keep their existing meanings.
Board/List/Swimlane status grouping uses concrete keys, with independent custom
columns, counts and cursors. Hidden/collapsed preferences preserve exact keys;
old category-named storage is read on load without merging sibling statuses.
New snapshots persist `hiddenStatuses`; exact status filters are unchanged.
Deploy the backend before clients: custom Swimlane columns require the compound
`secondary: status` API to accept workspace-scoped custom keys.
Validation errors (unknown/archived status, permissions, stale reorder sets)
still return their normal error responses; no API guarantees every request
will succeed, especially during the accepted mixed-version window.

## Regression coverage

- Real runner upgrade tests with pending and applied 469, 478 lock timeout,
  retry/replay, unchanged system/archived rows and released DDL locks. The
  historical 332-to-469 test remains as coverage of the preserved historical SQL.
- Real-runner old/mixed/already-backfilled upgrade matrices; contracted-storage
  seven-value legacy groups, explicit
  four-value groups, concrete status groups, per-bucket pagination and counts;
  visible parent lanes, format-bound cursors, cross-workspace isolation and GC.
- 100 historical terminal signals cannot starve a live recovery in another
  workspace; old custom Backlog retains the agreed non-parking semantics.
- Built-in identity, custom lifecycle vs special behavior, unknown-key handling,
  archived status reads, category filtering, table/swimlane grouping.
- Old/current category inputs and legacy response encoding, including realtime
  issue payloads; frontend normalization and mobile lifecycle parity.
- Backlog-to-custom-Unstarted promotion and no custom-to-Todo re-promotion;
  catalog locks, access checks, reorder atomicity, fixed built-in protection.
- Agent brief and bundled skill assertions enforce the new semantics.
