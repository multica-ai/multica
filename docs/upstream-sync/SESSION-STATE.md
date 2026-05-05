# Session State

**Format:** This file is the single source of truth for autonomous-loop execution. It is read at the start of every iteration and written at the end. Never edit during a running iteration.

## Current state

```yaml
status: NOT_STARTED          # NOT_STARTED | RUNNING | PAUSED | COMPLETED | HALTED
current_phase: "Phase -1"    # which phase we're in
current_task: "P1"           # which sub-task within phase
last_iteration_at: null      # ISO timestamp
total_iterations: 0
total_tokens_estimate: 0
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
  status: pending             # pending | in_progress | completed | failed
  preflight_checks:
    P1_module_augmentation: pending
    P2_path_alias_shadowing: pending
    P3_sqlc_multi_source: pending
    P4_migrations_multi_dir: pending
    P5_visual_regression: pending
    P6_lint_rule: pending
    P7_wrap_pattern_proof: pending
    P8_feature_flag: pending
  go_no_go: null

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
```

## Eval results (most recent per phase)

```yaml
phase_minus_1_evals: null
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
next_action: bootstrap        # bootstrap | execute_task | run_phase_eval | pause | halt
next_task_brief: |
  Initial state. The autonomous loop should:
  1. Read 03-decision.md "Phase -1 — Preflight" section
  2. Identify all 8 verification tasks
  3. Begin executing P1 (TypeScript module augmentation)
  4. Update this file to status=RUNNING, current_task=P1
```
