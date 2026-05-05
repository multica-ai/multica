# Step 8 — Autonomous execution protocol

**Mode:** Fully autonomous overnight execution. No user pauses except catastrophic stops.

**Wall-clock estimate:** 6-8 hours with parallel subagents.

**Token estimate:** 5-7M total.

**Final state:** Refactor complete through Phase 5; merged branch ready for user review and final landing to main.

## How the loop works

Each iteration:

1. **Read** `docs/upstream-sync/SESSION-STATE.md` to find current chunk
2. **Read** the chunk's brief in `07-session-schedule.md`
3. **Plan** sub-tasks; identify parallelization opportunities
4. **Dispatch** parallel subagents via Agent tool for independent tasks
5. **Eval** chunk after all sub-tasks complete (run `scripts/per-session-eval.sh`)
6. **Decide:**
   - All evals green → mark chunk complete; advance state to next chunk; ScheduleWakeup short delay (60-120s) to start next iteration
   - Eval failures → retry up to 3x within same iteration; if still failing, HALT
   - Catastrophic stop condition → HALT
   - Phase 5 complete → STOP (natural completion)

7. **Update** SESSION-STATE.md with current progress, decision log entry, eval results

## Autonomous decision protocol

Auto-decisions the loop takes WITHOUT user input:

| Situation | Action |
|---|---|
| All 8 preflight checks pass | Mark Phase -1 GO, proceed to Phase 0 |
| 7 of 8 preflight checks pass | Mark Phase -1 HOLD, document failure, proceed to Phase 0 with caveat |
| 6 or fewer preflight checks pass | Mark Phase -1 NO-GO, HALT |
| Phase 0 component fails | Retry once with revert; if still failing, HALT |
| Phase 1 feature migration fails | Skip that feature, log to deferred list, continue with others |
| Phase 2 inbox toggle E2E fails | Retry; if still failing, leave inbox in upstream-state with feature flag default-off, document, continue |
| Phase 3 wrapper too costly (>150 lines) | Convert to L3 patch, document, continue |
| Phase 4 marker addition fails | Skip file, log, continue (markers are documentation only) |
| Phase 5 merge has >30 conflicts | Mark NO-GO, HALT — strategy needs revisit |
| Phase 5 merge has 15-30 conflicts | Mark HOLD, complete what can be auto-resolved, document remainder for user |
| Phase 5 merge has <15 conflicts | Resolve all, run full eval, mark GO, present landing-ready branch |

## Parallelization rules

For each chunk, identify independent sub-tasks and dispatch as parallel subagents:

| Chunk | Subagents | Pattern |
|---|---|---|
| 1 (Phase -1) | 8 parallel | Each verification is independent throwaway-branch experiment |
| 2 (Phase 0a) | 1-2 | Skeleton creation is sequential; lint-rule + scripts can be parallel |
| 3 (Phase 0b) | 2-3 | Feature flag + Settings-tab can split; baselines parallel |
| 4 (Phase 1a) | 7 parallel | One subagent per package rename |
| 5 (Phase 1b) | 3 parallel | One per feature migration |
| 6 (Phase 1c) | 4 parallel | One per feature migration |
| 7 (Phase 2) | 1-2 | Cohesive end-to-end work |
| 8 (Phase 3a) | 4 parallel | One per wrapper |
| 9 (Phase 3b) | 4 parallel | One per wrapper |
| 10 (Phase 3c) | 5 parallel | One per wrapper |
| 11 (Phase 4) | 6 parallel | Each handles ~7 files |
| 12 (Phase 5) | 1 | Single atomic merge operation |

## Subagent brief template

When dispatching a subagent, the brief MUST include:

```
You are executing chunk <N>, sub-task <X>: <task name>.

CONTEXT:
- Working in worktree: /Users/hvejsel/firtal-repos/firtal-cerebro-upstream-sync
- Branch: <branch-name>
- Plan reference: docs/upstream-sync/03-decision.md, section "<section>"
- Audit reference: docs/upstream-sync/01-audit.md (file list for this task)

SCOPE (do exactly this, nothing else):
<concrete file list and operations>

DONE-CRITERIA:
<from 03-decision.md done-criteria for this chunk>

EVAL (run before reporting done):
<specific commands to verify your work>

OUTPUT:
- Final 1-page report: what you did, eval results, any issues encountered
- DO NOT update SESSION-STATE.md (orchestrator does that)
- Commit your work as atomic commits with conventional messages
```

## Eval execution

After every chunk, run:

```bash
scripts/per-session-eval.sh <chunk-id>
```

This script:
1. Runs `pnpm typecheck` (must pass)
2. Runs `pnpm test` (must pass)
3. Runs `make test` (Go tests, must pass)
4. Runs `pnpm exec playwright test` for relevant feature specs
5. Compares visual baselines (Playwright snapshot mode)
6. Validates CEREBRO-PATCH disciplin
7. Measures bundle size (must stay within 5% of baseline)
8. Outputs pass/fail table

If all green → chunk complete.
If any red → return diagnostic, retry up to 3x, then HALT.

## State file update protocol

After every chunk completion, the orchestrator updates SESSION-STATE.md:

```yaml
status: RUNNING
current_phase: <next-phase>
current_task: <next-task>
last_iteration_at: <ISO timestamp>
total_iterations: <N+1>
total_tokens_estimate: <accumulated>

phase_X:
  <task>: completed
  go_no_go: GO

decision_log:
  - "2026-05-05T22:45:00Z | Phase -1 | GO | All 8 preflight checks passed"
```

## Wake-up scheduling

After updating state:

```
If next chunk exists AND status != HALTED:
  ScheduleWakeup(delaySeconds=60, reason="continuing to chunk N+1", prompt="<<autonomous-loop-dynamic>>")
Else:
  Stop. Final state ready for user review.
```

60-120 second delays keep cache warm and give system time to settle between chunks. The loop self-paces — no fixed cadence.

## Recovery after HALT

If the loop HALTS:

1. Status is set to HALTED
2. SESSION-STATE.md captures exact failure point + diagnostic
3. Failure report written to `docs/upstream-sync/failures/<timestamp>.md`
4. User wakes, reads SESSION-STATE.md and failure report
5. User decides: fix manually + resume, or revert + replan

To resume after HALT, user provides new prompt: `Continue upstream-sync from HALTED state. Read SESSION-STATE.md and failures/<latest>.md, fix the failure, then resume the autonomous loop.`

## Final state user sees on waking

If everything went well:

```
SESSION-STATE.md:
  status: COMPLETED
  phase_5.go_no_go: GO
  phase_5.conflict_files: 8
  phase_5.conflict_lines: 23
  
Branch: chore/upstream-sync-validation
  Contains: full upstream/main merge with all conflicts resolved
  Ready: for user to review + push to main

PRs created (from earlier phases):
  - feat(cerebro): foundation + feature flag system
  - refactor(cerebro): rename isolated packages
  - feat(cerebro): inbox feature flag
  - refactor(cerebro): wrapper composition layer
  - chore(cerebro): patch markers + registry

Reports:
  - docs/upstream-sync/preflight/SUMMARY.md (Phase -1 results)
  - docs/upstream-sync/eval-reports/<chunk>.md (per-chunk evals)
  - docs/upstream-sync/sync-validation-report.md (Phase 5 final)
```

If something halted:

```
SESSION-STATE.md:
  status: HALTED
  pause_reason: <specific reason>
  
docs/upstream-sync/failures/<timestamp>.md
  - What was being attempted
  - What failed
  - Diagnostic output
  - Recommended next step
```

## What user sees in the morning

Best case: a branch ready to merge to main, eval reports showing everything green, and a one-line decision: "GO/NO-GO til at lande chore/upstream-sync-validation til main?"

Worst case: HALTED at a specific point with diagnostic info; user fixes one specific thing and resumes.

Either way: 6-8 hours of work done while user slept.

## Self-check checklist (orchestrator runs at end of each chunk)

```
[ ] All sub-task subagent reports collected
[ ] Eval-suite passed for this chunk
[ ] git status clean (no uncommitted changes orphaned)
[ ] git log shows expected commits
[ ] No unintended files modified
[ ] CEREBRO-PATCH disciplin held (no upstream-file mod without marker)
[ ] SESSION-STATE.md updated correctly
[ ] Decision log entry added
[ ] Token estimate updated
[ ] Next chunk identified OR completion reached
[ ] If continuing: ScheduleWakeup called with sentinel
[ ] If done/halted: Stop properly, write final reports
```

Only after ALL checklist items: report chunk done.

## Cost tracking

Maintain in SESSION-STATE.md:

```yaml
total_tokens_estimate: 0   # rough running total
budget: 7000000            # 7M token budget
```

Each chunk completion adds estimated tokens spent. If running total >budget×1.4, HALT for user review.

## Reference: full chunk dependency chain

```
Phase -1 (Chunk 1)
   ↓ GO
Phase 0a (Chunk 2) ─→ Phase 0b (Chunk 3)
                        ↓ GO
Phase 1a (Chunk 4) ─→ Phase 1b (Chunk 5) ─→ Phase 1c (Chunk 6)
                                              ↓ GO
                                            Phase 2 (Chunk 7)
                                              ↓ GO
Phase 3a (Chunk 8) ─→ Phase 3b (Chunk 9) ─→ Phase 3c (Chunk 10)
                                              ↓ GO
                                            Phase 4 (Chunk 11)
                                              ↓ GO
                                            Phase 5 (Chunk 12)
                                              ↓
                                            COMPLETED
```

12 chunks, mostly sequential with parallelization within each chunk.
