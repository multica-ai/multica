# Session State

**Format:** This file is the single source of truth for autonomous-loop execution. It is read at the start of every iteration and written at the end. Never edit during a running iteration.

## Current state

```yaml
status: RUNNING              # NOT_STARTED | RUNNING | PAUSED | COMPLETED | HALTED
current_phase: "Phase 0"     # which phase we're in
current_task: "0a_skeleton"  # which sub-task within phase
last_iteration_at: "2026-05-05T20:50:00Z"
total_iterations: 1
total_tokens_estimate: 515000
```

## Pause reason (if status = PAUSED)

```yaml
paused_at: null
pause_reason: null           # "phase_boundary" | "eval_failure_3x" | "user_review_required" | "irreversible_action"
awaiting: null               # what user input is needed
```

## Phase progress

```yaml
phase_minus_1:
  status: completed           # pending | in_progress | completed | failed
  preflight_checks:
    P1_module_augmentation: PASS
    P2_path_alias_shadowing: HOLD  # mitigation: barrel-shadow not literal subpath
    P3_sqlc_multi_source: PASS
    P4_migrations_multi_dir: PASS  # 9NNN files already exist; split is the work
    P5_visual_regression: HOLD     # mitigation: masks + Linux-CI baselines
    P6_lint_rule: PASS
    P7_wrap_pattern_proof: NO-GO   # comment-card → L3 (handled by escalation rule)
    P8_feature_flag: PASS
  go_no_go: HOLD
  amendments_required:
    - "P2: replace fictional @multica/views/issues/comment-card specifier with barrel @multica/views/issues/components"
    - "P5: Phase 0.6 budgets masks + Linux-CI baseline gen + deterministic data via TestApiClient"
    - "P7: drop comment-card from Phase 3 wrappers (14→13); add to Phase 4 markers (42→43)"
    - "P4: include git mv server/migrations/9*.sql server/migrations/cerebro/ as part of implementation"

phase_0:
  status: pending
  components:
    cerebro_zone_skeleton: pending
    feature_flag_system: pending
    lint_rule: pending
    sync_scripts: pending
    eval_baseline: pending
    claude_md_update: pending
  go_no_go: null

phase_1:
  status: pending
  features:
    rename_isolated_packages: pending  # 7 packages
    cerebro_test: pending
    cerebro_users: pending
    cerebro_notifications: pending
    cerebro_mcp: pending
    cerebro_realtime: pending
    cerebro_runtime: pending
    cerebro_api_subclient: pending
  go_no_go: null

phase_2:
  status: pending
  components:
    cerebro_inbox_extraction: pending
    feature_flag_routing: pending
    eval_inbox_baseline: pending
  go_no_go: null

phase_3:
  status: pending
  wrappers:
    comment_card: pending
    runtime_detail: pending
    project_detail: pending
    project_picker: pending
    chat_input: pending
    chat_message_list: pending
    issue_detail: pending
    agent_live_card: pending
    list_row: pending
    reply_input: pending
    readonly_content: pending
    tasks_tab: pending
    agents_page: pending
  go_no_go: null

phase_4:
  status: pending
  files_marked: 0
  total_files: 42
  go_no_go: null

phase_5:
  status: pending
  conflict_files: null
  conflict_lines: null
  go_no_go: null
  # Phase 5 executes the merge in chore/upstream-sync-validation branch.
  # Autonomous loop does NOT land merge to main — that requires user review.
  # Final state: merged branch + comprehensive go/no-go report ready for user.
```

## Branches in flight

```yaml
worktree_branch: chore/upstream-sync-analysis
phase_branches: []
open_prs: []
```

## Decision log

```
# Format: ISO_TIMESTAMP | PHASE | DECISION | RATIONALE
2026-05-05T20:50:00Z | Phase -1 | HOLD | 5 PASS, 2 HOLD with mitigations, 1 NO-GO (P7) handled by existing escalation rule. Strategy sound; plan amendments required before Phase 0.
2026-05-05T20:50:00Z | Phase -1 | proceed_to_phase_0 | Per autonomous protocol: 7 of 8 effective passes → HOLD → proceed with caveat documented in preflight/SUMMARY.md
```

## Eval results (most recent per phase)

```yaml
phase_minus_1_evals: pending  # baseline: trivial pass expected since no code changed
phase_0_evals: null
phase_1_evals: null
phase_2_evals: null
phase_3_evals: null
phase_4_evals: null
phase_5_evals: null
```

## Stop conditions (autonomous loop must check every iteration)

The loop must STOP and set status=HALTED ONLY when ANY of these triggers (catastrophic only — autonomous mode runs through all phase boundaries):

1. **Phase -1 NO-GO** — 3+ preflight verifications failed; strategy invalidated
2. **3+ consecutive eval failures within current chunk** — after retry attempts exhausted
3. **Total tokens estimate >10M** (well above 5-7M expected; runaway protection)
4. **Iteration count >50** (sanity limit)
5. **Catastrophic risk materialized** — DB corruption, infinite loop in user code, irreversible data loss
6. **Phase 5 merge done** — natural completion; final state ready for user review

**Phase boundaries are NOT pause points.** The loop progresses through Phase -1 → 0 → 1 → 2 → 3 → 4 → 5 autonomously as long as evals pass. Phase 5 ends with a merged branch ready for user to review and land manually — autonomous loop does NOT push to main or merge to main.

## Next action (autonomous loop reads this to decide what to do)

```yaml
next_action: execute_task     # bootstrap | execute_task | run_phase_eval | pause | halt
next_task_brief: |
  Phase 0a — chunk 2 per 07-session-schedule.md.
  Goal: build the cerebro-zone skeleton + lint-rule + sync-scripts foundation.
  Sub-tasks (1-2 parallel subagents, mostly sequential per protocol):
  1. Create empty package skeletons (12 packages/cerebro-*/ + their READMEs).
  2. Create server/internal/cerebro/* skeleton subdirs.
  3. Create server/migrations/cerebro/ directory + README documenting 9NNN reservation.
  4. Implement scripts/validate-cerebro-patches.sh + scripts/cerebro-zones.txt.
  5. Implement scripts/upstream-sync.sh skeleton (the merge automation).
  6. Wire scripts/validate-cerebro-patches.sh into .github/workflows/ci.yml as third job.
  7. Move 20 existing 9NNN_*.sql files from server/migrations/ into server/migrations/cerebro/ (per P4 note).
  8. Add `multi-dir scan` patch to server/cmd/migrate/main.go + idempotent_test.go (per P4 recommendation).
  9. Update CLAUDE.md with "Cerebro extension discipline" section.
  Eval: pnpm typecheck + make test + the new validate-cerebro-patches.sh must all pass.
  Branches: stay on chore/upstream-sync-analysis for now (no separate phase branch yet).
```
