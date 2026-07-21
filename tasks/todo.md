# MUL-4332 PR3 — matcher must-fix round (Elon review @ c4cfdcac8)

## Design decisions (from ratified description)

- §5.1: "matcher 在单事务快照内选择 revision" → fold [confirm lease ownership → select+pin
  candidate revisions → write decision/latch → finalize event] into ONE authoritative tx.
  Per-hook isolation inside it via SAVEPOINT (so #4 poison isolation survives #1's single tx).
- §15.3: "hop_count 上限 8；**超过**上限的候选记 skipped(max_depth)" → boundary is `> 8`,
  so hop_count == 8 FIRES and hop_count == 9 is rejected. Currently `>= 8` — wrong AND implicit.
- §5.2 reason codes: `max_depth` (was `hop_exceeded`), `condition_already_true`
  (was `edge_not_rising`), `condition_false` (unchanged).

## Fixes

- [x] #1 revision pinned in one authoritative tx
  - [x] `GetDomainEventForDispatch` (SELECT ... FOR UPDATE)
  - [x] `processEvent`: lease-verify → candidates → per-hook savepoints → finalize, one tx
  - [x] `MatchEvent` becomes the decision half, sharing the same in-tx routine
- [x] #2 lease CAS guards the decision writes, not just the last UPDATE
  - [x] fail-closed BEFORE any execution/latch write when lease is not ours
  - [x] finalize asserts affected rows == 1
  - [x] `MarkDomainEventDispatched` sets `dispatched_at = now()`
- [x] #3 depth guard must not skip the rising-edge latch advance
  - [x] latch advances to current condition state even when the guard rejects
  - [x] rename reason codes to `max_depth` / `condition_already_true`
  - [x] explicit `> maxHopCount` boundary
- [x] #4 poison hook must not starve healthy candidates
  - [x] `automation.ErrInvalidConfig` sentinel to classify deterministic config errors
  - [x] poison → terminal `failed` execution row, continue to remaining candidates
  - [x] transient (DB) errors still roll the whole event back for retry

## Deterministic race regressions (one per must-fix)

- [x] #1 `TestMatcherDecisionIsAtomicAcrossCandidates` — lock hook B's row; while the matcher
      blocks on it, hook A's decision must be INVISIBLE (old code committed it in its own tx)
- [x] #2 `TestMatcherStaleLeaseHolderWritesNothing` — stale lease holder writes no execution /
      no latch / does not finalize; true owner then succeeds with dispatched_at set
- [x] #3 `TestMatcherDepthGuardStillAdvancesLatch` — true → (over-deep false) → true must re-fire
      (+ `TestMatcherHopBoundary`: hop 8 fires, hop 9 skips max_depth)
- [x] #4 `TestMatcherPoisonHookDoesNotStarveHealthy` — poison first in order; healthy hook still
      queued, poison gets terminal `failed`, event still dispatched

## Verify

- [x] sqlc regen, build, vet, migration-lint
- [x] `-race` on internal/automation, internal/service (full pkg), internal/handler, cmd/server
- [x] push, CI green, one Chinese result comment

## NOT this slice

- executor (explicitly deferred by Trump until these four land)

## Review (this slice)

All four must-fixes landed, each with a regression proven load-bearing by reverting
the fix and observing the test fail:

| Must-fix | Mutation applied | Regression result |
|---|---|---|
| #1 | per-candidate txs instead of savepoints | FAIL — hook A visible before finalize |
| #2 | drop upfront lease check | FAIL — blocked on hook row (decided before checking) |
| #2 | drop finalize `rows == 1` | FAIL — stale holder wrote + reported dispatched |
| #3 | depth-guard early return | FAIL — latch stranded true |
| #4 | return on first candidate error | FAIL — event never dispatched |

Verification: clean DB migrated to 217; `go build ./...`, `go vet ./...` clean;
`-race` green on internal/automation, internal/service (full package, incl. the
shared-DB backlog condition), internal/handler, cmd/server. `cmd/multica` failures
are pre-existing and environmental (the agent runtime's daemon_task_context.json
marker); identical 134 FAIL lines on pristine HEAD.

No migration changes this slice — queries only, so no CONCURRENTLY concerns.
