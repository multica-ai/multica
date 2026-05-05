# Session State

**Format:** This file is the single source of truth for autonomous-loop execution. It is read at the start of every iteration and written at the end. Never edit during a running iteration.

## Current state

```yaml
status: RUNNING              # NOT_STARTED | RUNNING | PAUSED | COMPLETED | HALTED
current_phase: "Phase 0"     # which phase we're in
current_task: "0b_foundation" # which sub-task within phase (chunk 3)
last_iteration_at: "2026-05-05T21:11:00Z"
total_iterations: 2
total_tokens_estimate: 850000
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
  status: in_progress
  components:
    cerebro_zone_skeleton: completed   # 12 packages/cerebro-* + 13 server/internal/cerebro/* dirs + READMEs
    feature_flag_system: pending       # chunk 3
    lint_rule: completed               # scripts/validate-cerebro-patches.sh + cerebro-zones.txt + ci.yml job
    sync_scripts: completed            # scripts/upstream-sync.sh skeleton with --help; full impl chunk 3+
    eval_baseline: pending             # chunk 3 (visual + API contract + perf)
    claude_md_update: pending          # chunk 3
    multi_dir_migrate: pending         # chunk 3 (per P4 amendment — couples with 9NNN move)
  go_no_go: null                       # set when all components complete (after chunk 3)
  chunk_2_eval: PASS

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
2026-05-05T21:11:00Z | Phase 0a | chunk_2_GO | All eval checks PASS (typecheck, vitest, Go tests, cerebro-patches script). 12 cerebro-* package skeletons + server/internal/cerebro/* + scripts/validate-cerebro-patches.sh + scripts/upstream-sync.sh skeleton + CI guard job landed. Bash 3.2 compat fix to per-session-eval.sh.
2026-05-05T21:11:00Z | Phase 0a | proceed_to_chunk_3 | Foundation skeleton complete; chunk 3 now adds feature flags + multi-dir migrate + 9NNN move + CLAUDE.md.
```

## Eval results (most recent per phase)

```yaml
phase_minus_1_evals: N/A  # docs-only chunk; see eval-reports/chunk-1-20260505T205000Z.md
phase_0_chunk_2_evals: PASS  # see eval-reports/chunk-2-20260505T191025Z.md
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
  Phase 0b — chunk 3 per 07-session-schedule.md.
  Scope per the schedule: "Feature flag-system + Settings-tab + eval-baselines + CLAUDE.md".

  Sub-tasks (2-3 parallel subagents):
  A. Frontend feature flag (packages/cerebro-feature-flags) per docs/upstream-sync/preflight/P8.md:
     - registry.ts (flag keys + defaults — initial: cerebro_inbox, cerebro_access_control, cerebro_members_admin, cerebro_sandbox_ui, cerebro_mcp_guide; all default-on)
     - store.ts (Zustand + persist + workspace-aware storage; model after packages/core/issues/stores/comment-collapse-store.ts)
     - api.ts (TanStack Query hooks useFeatureFlag/useSetFeatureFlag using existing api client)
     - settings-tab.tsx (ExtraSettingsTab content with toggle UI)
     - index.ts (exports + featureFlagTabs array)
     - Wire into apps/web settings page + apps/desktop routes.tsx (merge with existing Daemon tab)
     - Add storage cleanup entry in packages/core/platform/storage-cleanup.ts
  B. Backend feature flag (server/internal/cerebro/feature_flags) per P8:
     - server/internal/cerebro/feature_flags/handler.go (GET/PUT, default-on semantics via LEFT JOIN)
     - server/internal/cerebro/queries/feature_flags.sql for sqlc (Get / Upsert / ListForUser)
     - WS broadcast on PUT (feature_flag:changed payload {key, enabled})
     - Update sqlc.yaml to add cerebro package per P3 (uses server/internal/cerebro/queries/ + server/internal/cerebro/db/migrations/)
     - server/migrations/9010_cerebro_feature_flags.up.sql (lives in server/migrations/, NOT cerebro/ subdir — multi-dir migrate is deferred)
     - Schema: cerebro_feature_flags(workspace_id uuid, user_id uuid, flag_key text, enabled bool, updated_at, PK(workspace_id, user_id, flag_key))
  C. CLAUDE.md update per 03-decision.md Phase 0.7:
     - Add "Cerebro extension discipline" section (4 numbered points: cerebro-* zone, CEREBRO-PATCH markers ≤5 lines, 9NNN_cerebro_* migration namespace, feature-flag every cerebro feature)
     - Reference scripts/validate-cerebro-patches.sh + .eslintrc as enforcement
  D. Eval baseline (per P5 amendment — semantic E2E only, defer pixel snapshots):
     - Add data-testid="<page>-loaded" hooks where missing on inbox/projects/settings (already has many testids)
     - Document fallback: pixel snapshots deferred to chunk 4+ until masks proven stable
     - Capture API contract baseline if straightforward (run existing E2E specs once and snapshot key responses)

  Explicit non-scope (deferred to later chunks):
  - Multi-dir migrate patch (server/cmd/migrate/main.go) — defer to chunk 11 (Phase 4) when 9NNN move actually happens
  - 9NNN file move from server/migrations/ to server/migrations/cerebro/ — defer to chunk 11 alongside multi-dir patch
  - PR creation against main — deferred until Phase 1 done; chore/upstream-sync-analysis stays as working branch

  Eval gate: pnpm typecheck + pnpm test + make test + cerebro-patches all PASS.
  Manual smoke: feature flag UI loads in settings; toggle persists to server; Settings-tab renders the cerebro section.
  Branches: stay on chore/upstream-sync-analysis.
  Estimated tokens: ~500-700k (large chunk; A+B can run parallel, C+D smaller).
```
