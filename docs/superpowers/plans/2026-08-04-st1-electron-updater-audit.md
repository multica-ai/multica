# ST-1 Electron Updater Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a non-destructive, machine-verifiable monthly ST-1 audit of Electron updater residue to the existing storage-governance worker.

**Architecture:** A bounded scanner in `retention_worker.py` reads explicit host-relative globs, emits one atomic report per Shanghai calendar month, and is invoked before external-volume checks by the existing formal cron worker. A manual audit-only mode commissions the same code path; no second scheduler or cleanup owner is introduced.

**Tech Stack:** Python 3 standard library (`glob`, `os`, `pathlib`, `zoneinfo`, `unittest`), JSON configuration, existing cron-to-FDA storage-governance bridge.

---

### Task 1: Specify scanner behavior with failing tests

**Files:**
- Modify: `scripts/storage-governance/test_retention_worker.py`
- Test: `scripts/storage-governance/test_retention_worker.py`

- [ ] **Step 1: Add imports and fixtures**

Import `audit_electron_updaters` and `maybe_run_electron_updater_audit`. Build a
temporary home with an updater directory, a pending ZIP, and a symlink to an
out-of-root payload.

- [ ] **Step 2: Assert discovery and safety behavior**

Assert that the report de-duplicates overlapping globs, sums regular-file
payload, reports the real candidates, records the symlink as an error without
following it, and never changes source paths.

- [ ] **Step 3: Assert threshold and monthly-gate behavior**

Use an injected UTC time whose Shanghai date is before and after the configured
day. Assert `skipped` before the day, a report on/after the day, `skipped` when
valid current-month evidence exists, and a rerun with `force=True`.

- [ ] **Step 4: Run the focused tests and verify RED**

Run: `python3 -m unittest scripts.storage-governance.test_retention_worker.ElectronUpdaterAuditTest -v`

Expected: import failure because the audit functions do not exist.

### Task 2: Implement the scanner and monthly evidence gate

**Files:**
- Modify: `scripts/storage-governance/retention_worker.py`
- Test: `scripts/storage-governance/test_retention_worker.py`

- [ ] **Step 1: Add the audit functions**

Implement:

```python
def audit_electron_updaters(
    config: Dict[str, Any],
    *,
    now: Callable[[], datetime] = utc_now,
    invocation_source: str = "manual",
) -> Dict[str, Any]: ...

def maybe_run_electron_updater_audit(
    config: Dict[str, Any],
    *,
    now: Callable[[], datetime] = utc_now,
    force: bool = False,
    invocation_source: str = "manual",
) -> Dict[str, Any]: ...
```

Resolve the configured timezone with `ZoneInfo`, validate every match beneath
`home_path`, scan without following symlinks, atomically write the month-named
JSON report, and classify `green`, `attention`, or `red` from scan errors and
configured thresholds.

- [ ] **Step 2: Run the focused tests and verify GREEN**

Run: `python3 -m unittest scripts.storage-governance.test_retention_worker.ElectronUpdaterAuditTest -v`

Expected: all `ElectronUpdaterAuditTest` cases pass.

- [ ] **Step 3: Run the complete storage-governance tests**

Run: `python3 -m unittest discover -s scripts/storage-governance -p 'test_*.py' -v`

Expected: all tests pass with zero failures.

### Task 3: Integrate scheduling and audit-only CLI

**Files:**
- Modify: `scripts/storage-governance/retention_worker.py`
- Modify: `scripts/storage-governance/test_retention_worker.py`

- [ ] **Step 1: Add failing integration tests**

Patch external-volume construction and assert `run_worker` invokes the monthly
audit before external-volume checks. Add an argument-parser test or subprocess
test proving `--electron-audit-only` does not require cron lineage or external
storage.

- [ ] **Step 2: Verify RED**

Run the two new test names directly and confirm they fail because the formal and
CLI integration is absent.

- [ ] **Step 3: Implement the integration**

Call `maybe_run_electron_updater_audit` immediately after formal cron lineage
verification, attach its summary to the retention report, alert once when a
formal scan returns `attention`, and fail closed on `red`. Add
`--electron-audit-only` to `main`; this mode takes the existing lock, forces the
audit, prints a compact JSON result, and exits nonzero only for `red`.

- [ ] **Step 4: Verify GREEN**

Run the focused tests, then the complete storage-governance suite.

### Task 4: Configure, document, deploy, and commission

**Files:**
- Modify: `scripts/storage-governance/retention-config.example.json`
- Modify: `scripts/storage-governance/README.md`
- Modify: `/Users/tangyuanjc/.local/libexec/storage-governance/retention_worker.py` (deployed copy)
- Modify: `/Users/tangyuanjc/.local/libexec/storage-governance/retention-config.json` (deployed config)
- Create: `/Users/tangyuanjc/.org/metrics/st1-electron-updater-audit-2026-08.json` (runtime evidence)

- [ ] **Step 1: Add the host-neutral example config**

Document `enabled`, `timezone`, `day_of_month`, `home_path`, `report_dir`,
bounded `patterns`, `warn_total_gib`, `warn_candidate_gib`, `stale_days`, and
`stale_min_mib`.

- [ ] **Step 2: Update the runbook**

State that ST-1 is monthly-gated inside the formal retention owner, is strictly
read-only, produces month-named evidence, and can be commissioned with
`--electron-audit-only`.

- [ ] **Step 3: Deploy reviewed files and config**

Copy the reviewed worker to the existing libexec location, update only the
`electron_updater_audit` config object, preserve executable mode, and validate
both JSON files.

- [ ] **Step 4: Run the real audit**

Run:

```bash
/usr/bin/python3 /Users/tangyuanjc/.local/libexec/storage-governance/retention_worker.py \
  --config /Users/tangyuanjc/.local/libexec/storage-governance/retention-config.json \
  --electron-audit-only
```

Expected: exit 0 with `green` or `attention`, plus a current-month evidence
path. Validate the report with `python3 -m json.tool`, confirm its mtime is in
the current Shanghai month, and confirm no candidate has `action: deleted` or
`action: moved` because the schema exposes observations only.

### Task 5: Final verification and delivery

**Files:**
- Verify all modified files

- [ ] **Step 1: Run fresh verification**

Run the full Python suite, `python3 -m py_compile` on all storage-governance
Python files, `python3 -m json.tool` on example and deployed configs and the
monthly report, `cmp` on repository/deployed worker, and `git diff --check`.

- [ ] **Step 2: Commit and update the open storage-governance PR**

Create atomic conventional commits, push them to the existing PR #6281 head,
and add `Closes WS-3010` to that PR body so this follow-up is linked and closes
on merge.

- [ ] **Step 3: Deliver Multica evidence**

Create the required Allen closing sub-issue, post exactly one concise WS-3010
result comment with the audit command, exit code, raw compact output, report
summary, test count, and PR URL, then move WS-3010 to `in_review`.
