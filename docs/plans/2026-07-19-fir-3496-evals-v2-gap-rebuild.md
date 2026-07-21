---
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
execution: code
title: "Evals v2 — gap-plan rebuild on the block-chain engine (FIR-3496)"
issue: FIR-3496
created: 2026-07-19
owner: Sabine
flags: [cerebro_evals, cerebro_workflows, cerebro_workflow_hooks, CEREBRO_EVAL_DRIFT_ENABLED]
---

# Evals v2 — gap-plan rebuild on the block-chain engine (FIR-3496)

## Goal Capsule

Re-implement the three remaining Evals v2 features — **(1) evals coupled to Hooks, (2) plan/monitor phases + Block-vs-Warn on Workflows, (3) drift: schedule + alarms + owner/rights** — on top of the NEW block-chain workflow engine that landed via FIR-3493 (#2483). The previous build of these features lives on `release/fir-3496-evals-v2-production-20260718` but was written against the OLD `loops` engine (`spec.go` + `compile.go`, now deleted), so it cannot be merged and must be rebuilt. Every path stays behind its existing default-OFF feature flag. Done = `make check` green on a fresh checkout, each user-facing surface covered by an automated test, and the branch merged to `main` (staging) via CI.

## Why a rebuild (grounded in code)

- FIR-3493 (`2c69cc800`, merged 2026-07-19T06:00Z) rewrote the engine into block chains: `loops/blocks.go` (`Phase > blocks > steps > gates`), `loops/chain_driver.go`, `loops/chain_store.go`, `loops/dispatch.go`, `loops/materialize_issue_loop.go`. It **deleted** `loops/spec.go` and `loops/compile.go`.
- The gap-plan branch patches those deleted files (Theme 2 plan/monitor emission) → not cherry-pickable.
- **Two parallel eval-gate mechanisms now exist on main, and they do NOT share phase logic:**
  - **NEW block-chain path (live for issue-loops):** a `BlockEval` block → `evals.BlockRunner.RunEvalBlock` (`server/internal/cerebro/evals/block_runner.go:26-49`). It is **phase-blind** and selects only `b.blocking=true` bindings (`block_runner.go:31`). Wired at `server/cmd/server/router.go:854-856`.
  - **LEGACY condition path (still wired):** `check_passes` → `evals.GateEvaluator.EvaluateCheckGate` (`evals/gate.go:35-49`) → `evals.Store.BlockingEvalsPassed(ctx, workflowID, issueID, phase)` (`evals/store.go:272-294`). This IS phase-aware (`validEvalPhase` = `plan|delivery|monitor`) and already on main. Wired at `server/cmd/server/main.go:360-361`.
- **Already on main — do NOT rebuild:** phase-aware blocking (`BlockingEvalsPassed(...,phase)`), `validEvalPhase`, `CheckGateConfig.EvalPhase` + `evalPhaseCarrier` (`loops/gate_types.go`), phase derivation in `evals/gate.go`. Theme 2 commits `241660eb8` and `0d2762891` are effectively satisfied by main; only advisory/monitor/UI are genuine gaps.
- The hook action executor **does** have DB access on new main (`workflows/hook_action_postgres.go:18-29`, `PostgresHookActionExecutor{db cerebrodb.DBTX}`). The old plan's premise "hooks have no DB" is outdated — no new DB seam is needed for hook actions; only the pure hook-engine **condition** path lacks a resolver.

## Requirements (traceable)

| # | Requirement | Source |
|---|---|---|
| R1 | A Hook can run an eval (`eval.run`) and gate on one (`eval.gate`, fail-closed) | FIR-3496 Fase 4 Hooks |
| R2 | A Hook policy can condition on `eval_passed` (DB-backed, fail-closed) | FIR-3496 Fase 4 |
| R3 | Hook editor UI exposes the two eval actions | FIR-3496 Fase 4 |
| R4 | Workflow eval bindings support `plan`/`delivery`/`monitor` phase on the LIVE engine, not only the legacy condition path | FIR-3496 Fase 4 Workflows |
| R5 | Advisory (`blocking=false`) bindings WARN (notify owner/admins) instead of blocking | FIR-3496 Fase 4 |
| R6 | Monitor-phase eval advisory fires after delivery, warn-only, never reverts | FIR-3496 Fase 4 |
| R7 | Binding UI lets a user choose phase + Block/Warn | FIR-3496 Fase 4 skærm 6 |
| R8 | Evals run on a schedule (weekly/daily/on-change/manual) via a sweeper | FIR-3496 Fase 5 |
| R9 | Alarm on eval fail and on pass-rate regression vs previous version | FIR-3496 Fase 5 |
| R10 | Only the eval owner or a workspace admin may edit an eval | FIR-3496 Fase 5 |
| R11 | Only admins/granted members may set a BLOCKING eval gate | FIR-3496 Fase 5 |

## Scope Boundaries (non-goals)

- Do NOT re-port Theme 2's phase-blocking commits verbatim — main already has `BlockingEvalsPassed(...,phase)` + `validEvalPhase` + `CheckGateConfig.EvalPhase`. Reuse them.
- Do NOT touch `firtal-evals`' role as the file-based asset store.
- Do NOT edit upstream-zone files except via `// CEREBRO-PATCH(<name>)` (≤5 lines) documented in `docs/cerebro-patches.md`. All new code lands in `packages/cerebro-*/`, `server/internal/cerebro/*`, `server/cmd/server/*` glue, and `9NNN_cerebro_*` migrations.
- Do NOT flip any feature flag ON in this branch. Flags stay default-OFF; enabling is a separate deploy/rollout step.
- No prod deploy in this plan — Definition of Done ends at merge to `main` (staging) with green CI.

## Key architecture decision — DECIDED (validate at review)

**Q: The live block-chain `BlockEval` is phase-blind and blocking-only. Do we make phases/advisory work on the live engine, or only on the legacy `check_passes` condition path?**

**Decision: A — make it work on BOTH.** Extend `BlockRunner.RunEvalBlock` to be phase-aware and to emit an advisory (warn, non-blocking) outcome, so the live issue-loop engine honors plan/monitor/advisory; keep the legacy condition path's phase logic that main already has. Rationale: Option B (legacy path only) leaves the live engine's eval blocks phase-blind — a half-feature that would confuse users who set a "monitor / warn-only" binding and see it silently ignored by the running workflow. "Right over fast" — the extra code in `block_runner.go` + `chain_driver.go` is the correct long-term shape. This is the single non-trivial engine extension; see U8. If review judges the block-chain extension too risky for this pass, the fallback is B (ship U1-U7, U9-U13; defer U8) — but that must be a conscious, logged choice, not silent.

## Verification Contract / Definition of Done

- `make check` (typecheck + `pnpm test` + `make test` + Playwright E2E) green on a fresh checkout of this branch.
- Every implementation unit ships with the tests named in its section; each behavior test fails before the unit's code and passes after.
- Fail-closed invariant holds: `eval.gate`, `eval_passed`, and every `BlockingEvals*` read return "blocked" when there is no passing run. Advisory/monitor/drift are warn-only and never change a gate's advance decision (proven by test).
- The two user-facing surfaces (hook editor eval actions; GateBinder phase + Block/Warn) each have an automated test driving the control to its outcome.
- Branch merged to `main` via a `prod-ready`/`staging-only`-labeled PR with green CI. Flags remain OFF.

---

## Implementation Units

Reference commits are on `release/fir-3496-evals-v2-production-20260718`; use them for INTENT, not verbatim (engine changed). All file paths repo-relative.

### THEME 1 — Evals ⇄ Hooks  (R1, R2, R3)

#### U1 — `eval.gate` hook action + `LatestRunPassed` store query  (ref `7b44cd5be`)
- **Files:** `server/internal/cerebro/evals/store.go` (add `LatestRunPassed`, `EvalPassed` alias), `server/internal/cerebro/workflows/hook_actions.go` (register type, validation, capability), `server/internal/cerebro/workflows/hook_action_postgres.go` (switch case + `startEvalGate`).
- **Decision:** `eval.gate` READS the latest verdict, does not execute. `Store.LatestRunPassed(ctx, workspaceID, evalID, issueID)` = `COALESCE((SELECT status='passed' FROM cerebro_eval_run WHERE workspace_id/eval_id/issue_id ORDER BY created_at DESC LIMIT 1), FALSE)` — keyed on eval+issue (NOT workflow). `startEvalGate` returns a sentinel `ErrHookGateBlocked` when not passed; a fail-closed policy turns the recorded action-failure into a `HookBlock`.
- **Pattern:** mirror `judge.gate` across all five spots — type list `hook_actions.go:64`, validation `hook_actions.go:129`, capability `hook_actions.go:166` (`trigger_other_agent`), registration `hook_actions.go:185`, executor switch `hook_action_postgres.go:88` + impl `startJudgeGate` `:173-218`.
- **Tests:** `evals/store_db_test.go::TestLatestRunPassed` (newest-passed→true / newest-failed→false / no-runs→false). `workflows/hook_action_postgres_test.go::TestEvalGateActionBlocksOnFailedRun` (failed→`HookActionFailed`+`HookBlock`; passed→success+not blocked).
- **Verify:** `cd server && go test ./internal/cerebro/evals/ ./internal/cerebro/workflows/ -run 'LatestRunPassed|EvalGate'`.

#### U2 — `eval.run` hook action + shared run-executor resolver  (ref `d9fcfb3ca`)
- **Files:** `server/internal/cerebro/workflows/hook_actions.go`, `hook_action_postgres.go` (add `evalStore *evals.Store` + `evalRunner` fields, `startEvalRun`), `server/internal/cerebro/workflows/hook_feature.go` (thread store+runner), `server/cmd/server/router.go` (extract the run-executor resolver used by the evals handler and pass it into `NewHookFeature`).
- **Decision:** `eval.run` EXECUTES the eval. Resolve the workspace's own runner via the SAME per-workspace Firtal Gateway path as eval "Run-now" (do not invent a new credential path — reuse the resolver from `router.go`, see how `WithExecutorResolver` / `cerebroevalrun.New(pool)` is built at `router.go:845`). Persist via `evalStore.CreateRun`. Nil-safe: fail closed when store/runner nil or no gateway for workspace.
- **Pattern:** `startJudgeGate` enqueue pattern; executor already has `e.db cerebrodb.DBTX` (`hook_action_postgres.go:18-29`).
- **Tests:** `workflows/hook_action_postgres_test.go::TestEvalRunActionExecutes` (seed eval, assert persisted run status). `workflows/hook_actions_test.go::TestEvalRunActionValidationAndRegistry` (registry + capability + missing `eval_id` rejected).
- **Verify:** `go test ./internal/cerebro/workflows/ -run 'EvalRun'`.
- **Depends on:** U1 (shares executor struct changes).

#### U3 — `eval_passed` DB-backed hook condition + resolver seam  (ref `221338408`)
- **Files:** `server/internal/cerebro/workflows/conditions.go` (`const OpEvalPassed = "eval_passed"`), NEW `server/internal/cerebro/workflows/hook_eval_condition.go` (was deleted; `HookConditionResolver` iface, `splitHookConditions`, `resolveHookConditions`), `server/internal/cerebro/workflows/hook_engine.go` (`resolver` field + `WithConditionResolver`, `Evaluate` splits pure/deferred), `server/internal/cerebro/evals/store.go` (`EvalPassed` alias of `LatestRunPassed`), `hook_feature.go` (`if evalStore != nil { engine = engine.WithConditionResolver(evalStore) }`).
- **Decision:** The pure hook engine (`hook_engine.go:81` → `evaluate()` in `conditions.go:49-106`) has NO DB. Add a resolver seam ONLY for deferred ops; `eval_passed` is conjunctive and **fails closed** on nil resolver / missing workspace or issue / unparseable eval id / resolver error. Guard the interface assignment so a nil `*Store` never becomes a non-nil interface.
- **Pattern:** the Service/`conditionsHold` split of pure vs deferred (`workflows/service.go:496-560`, `OpEvidencePresent`/`OpCheckPasses`) is the reference shape; reuse `Store.LatestRunPassed` as the query body.
- **Tests:** `workflows/hook_eval_condition_test.go`: `TestSplitHookConditions`, `TestResolveHookConditions` (true/false/error-fails-closed/nil-fails-closed/no-deferred-vacuously-true/missing-issue-fails-closed), `TestHookEngineEvalPassedConditionGatesActions`.
- **Verify:** `go test ./internal/cerebro/workflows/ -run 'HookCondition|EvalPassed|SplitHook'`.

#### U4 — Hook editor exposes eval actions (frontend)  (ref `995d31e7a`)
- **Files:** `packages/cerebro-workflows/core/hook-ux.ts` (add `"eval"` to `target` union; `ACTION_CONFIGURATION["eval.run"]` + `["eval.gate"]`, each field `{key:"eval_id", input:"target", target:"eval", required:true}`), `packages/cerebro-workflows/views/hooks/hook-step-panel.tsx` (add to `ACTION_OPTIONS`/action select), `packages/cerebro-workflows/views/hooks/hook-target-picker.tsx` (add `"eval"` to `HookDirectory`), `packages/cerebro-workflows/views/hooks/use-hook-directory.ts` (fetch evals inline via `api.cerebroRequest("/api/cerebro/evals")` — NOT via `@multica/cerebro-evals`, keep deps one-directional).
- **Pattern:** the `judge.gate` frontend entry `hook-ux.ts:63-66`.
- **Tests:** `packages/cerebro-workflows/core/hook-ux.test.ts` + `views/hooks/hook-action-options.test.ts` (both actions target `eval`, required). Playwright E2E: add an `eval.gate` action in the hook editor and save.
- **Verify:** `pnpm --filter @multica/cerebro-workflows exec vitest run`.

### THEME 2 — Phases + advisory (Block vs Warn)  (R4, R5, R6, R7)

#### U5 — `FailingAdvisoryEvals` query + `AdvisoryWarner`  (ref `241660eb8` partial + `5a2aa171e`)
- **Files:** `server/internal/cerebro/evals/store.go` (add `FailingAdvisoryEvals(ctx, workflowID, issueID, phase) ([]Binding, error)` — mirror of `BlockingEvalsPassed` for `NOT b.blocking` bindings whose latest run ≠ passed, joined to `cerebro_eval` for key/version/title, newest first), NEW `server/internal/cerebro/evals/advisory.go` (`AdvisoryWarner{store, recipients, inbox, bus}` + `Warn(ctx, workflowID, issueID, phase)`), `server/internal/cerebro/evals/gate.go` (`WithAdvisoryWarner`, call `warner.Warn` after base checks pass, log-and-continue, BEFORE the blocking check), `server/cmd/server/main.go` (build `NewAdvisoryWarner`, share one `evalGateStore`; CEREBRO-PATCH tag `main-eval-advisory`).
- **Decision:** advisory NEVER blocks — callers ignore its error for gating. Inbox card `type="eval_advisory_failed"`, `severity="attention"`, system actor, `route="inbox"`, details JSON `{eval,workflow,issue,phase,blocking:false}`; live fan-out via `bus.Publish(EventInboxNew)` per recipient. Model on `driftwatch/sweeper.go` inbox writing.
- **Tests:** `evals/store_db_test.go` (FailingAdvisoryEvals: returns non-blocking failing bindings, excludes blocking + passing), `evals/advisory_test.go` (one card per owner/admin; no recipients/no inbox = no-op; per-card failure skipped not fatal; never affects gate decision).
- **Verify:** `go test ./internal/cerebro/evals/ -run 'Advisory|FailingAdvisory'`.

#### U6 — Monitor-phase advisory after delivery, re-expressed on the new engine  (ref `ca9fdaeab`)
- **Files:** `server/internal/cerebro/loops/materialize_issue_loop.go` (emit a monitor rule/phase), reuse `loops/gate_types.go` `CheckGateConfig{EvalPhase:"monitor"}` + `evals/gate.go`.
- **Decision:** the old `Spec.Monitor`/`Compile()` emission is gone. Re-express: after the delivery phase, materialize a monitor step whose gate uses the legacy `check_passes` config with `EvalPhase:"monitor"`, wired through `evals.GateEvaluator` so the AdvisoryWarner (U5) notifies owners on a failing monitor eval. Monitor evals ONLY warn — no `RevertStatus`, no reopen, must not self-trigger (do not re-emit a status-changed event). Keep `done` terminal.
- **Deferred to implementation:** exact insertion point in `materialize_issue_loop.go` (`:105-123` `ActivateOnIssue` / `advanceChain`) — confirm whether a monitor step is a new `Phase` bound to `DoneStatus` or a post-terminal hook. Resolve by reading the materializer during execution.
- **Tests:** an integration test that drives an issue to done with a failing monitor-phase advisory binding and asserts (a) the issue stays done, (b) an advisory inbox card is written. Place in `loops/` or `evals/` per where the emission lands.
- **Verify:** `go test ./internal/cerebro/loops/ ./internal/cerebro/evals/ -run 'Monitor'`.
- **Depends on:** U5.

#### U7 — GateBinder UI: choose phase + Block/Warn  (ref `02c4b7366`)
- **Files:** NEW `packages/cerebro-evals/views/gate-binder.tsx` (`<GateBinder>` selects eval, Issue workflow, phase `plan|delivery|monitor`, enforcement `Block`|`Warn only`; emits `onBind({workflowId, evalId, phase, blocking})`; button label "Add blocking gate" / "Add advisory gate"), `packages/cerebro-evals/views/evals-page.tsx` (replace the hardcoded `phase:"delivery", blocking:true` binder with `<GateBinder>`; `bindMutation` takes the 4 fields), `packages/cerebro-evals/api.ts` (ensure `EvalBindingPhase` type + binding create passes phase+blocking; `updateEval`/`createBinding` already exist).
- **Pattern:** existing binder block in `evals-page.tsx`; existing `createBinding` API method.
- **Tests:** `packages/cerebro-evals/views/gate-binder.test.tsx` (selecting Warn-only sets `blocking:false`; phase select feeds the mutation). Playwright E2E: bind an eval as "monitor / Warn only" and assert the binding persists with `blocking=false`.
- **Verify:** `pnpm --filter @multica/cerebro-evals exec vitest run`.

#### U8 — Block-chain `BlockEval` becomes phase-aware + advisory-capable  (new — the architecture decision, ref intent from Theme 2)
- **Files:** `server/internal/cerebro/loops/blocks.go` (add `EvalPhase` field to `Block`, extend `validateBlock` BlockEval case `:300-303`), `server/internal/cerebro/loops/chain_driver.go` (thread phase through `BlockDispatch` `:49-54`), `server/internal/cerebro/loops/dispatch.go` (pass phase into `RunEvalBlock` `:200-205`), `server/internal/cerebro/evals/block_runner.go` (add phase predicate to the binding SQL `:31`; when the resolved binding is `blocking=false`, return `StepCompleted` + a warning outcome instead of `StepFailed`), and if a distinct advisory status is introduced, the `switch result.Status` in `chain_driver.advancePhase` `:205-218`.
- **Decision:** implements decision A above — the LIVE engine honors plan/monitor phases and warn-only bindings. Advisory bindings resolved here call the same `AdvisoryWarner` (U5) rather than failing the step.
- **Tests:** `evals/block_runner_test.go` (blocking binding fails step on non-pass; advisory binding returns completed+warning; phase predicate selects the right binding). `loops/chain_driver_test.go` (advisory eval does not fail the phase).
- **Verify:** `go test ./internal/cerebro/loops/ ./internal/cerebro/evals/ -run 'BlockEval|Advisory|Phase'`.
- **Depends on:** U5. **Risk:** highest-risk unit (touches the new engine core) — see "Fallback" in the architecture decision.

### THEME 3 — Drift: schedule, alarms, owner/rights  (R8, R9, R10, R11)

Whole theme behind env `CEREBRO_EVAL_DRIFT_ENABLED` (default OFF): sweepers `return` immediately until enabled.

#### U9 — Eval schedule table + store  (ref `8e2a5b9b3`)
- **Files:** NEW `server/migrations/9148_cerebro_eval_schedule.up.sql` + `.down.sql` (RENUMBERED from gap-plan's 9147; 9147 is taken by `9147_cerebro_issue_loop_chain_cutover`), NEW `server/internal/cerebro/evals/schedule_store.go`.
- **Schema (9148):** `cerebro_eval_schedule(id, workspace_id→workspace ON DELETE CASCADE, eval_id→cerebro_eval ON DELETE CASCADE, schedule_expr TEXT CHECK non-empty, timezone TEXT DEFAULT '', enabled BOOL DEFAULT TRUE, next_run_at TIMESTAMPTZ, last_run_at TIMESTAMPTZ, created_by_id UUID, created_at, UNIQUE(eval_id))` + `idx_cerebro_eval_schedule_due(enabled, next_run_at)`.
- **Store:** cron via `robfig/cron/v3` (5-field + descriptors, same config as the workflow cron sweeper), default TZ `Europe/Copenhagen`. `UpsertSchedule` (`ON CONFLICT (eval_id) DO UPDATE`, anchor `next_run_at` on `now()` — never backfill), `ClaimDueSchedules(now, limit)` (non-mutating read), `MarkScheduleRan(id, next)`, `EvalSchedule.NextRun(from)`.
- **Tests:** `evals/schedule_migration_test.go` (idempotent up/down), `evals/schedule_store_db_test.go` (upsert one-per-eval, claim-due ordering, next-run advance, no backfill).
- **Verify:** `make migrate-up && go test ./internal/cerebro/evals/ -run 'Schedule'`.

#### U10 — Scheduled runs via sweeper  (ref `2a9114dfd`)
- **Files:** NEW `server/internal/cerebro/evals/schedule_sweeper.go`, NEW `server/cmd/server/cerebro_eval_drift.go` (`buildEvalRunExecutorResolver(queries)` — same per-workspace Firtal Gateway resolution as Run-now/eval.run; nil when no gateway), `server/cmd/server/main.go` (`go NewScheduleSweeper(...).Run(sweepCtx, time.Minute)`; CEREBRO-PATCH `main-eval-schedule-sweeper`).
- **Decision:** production interval 1 minute; no-op when drift disabled. `SweepOnce`: `ClaimDueSchedules(now,100)`, run each; one bad schedule never blocks the rest. `runOne`: if no gateway executor for the workspace, still `MarkScheduleRan` (advance, don't hot-loop). Idempotent, never backfills.
- **Tests:** `evals/schedule_sweeper_test.go` (runs a due schedule; missing-gateway still advances; disabled = no-op).
- **Verify:** `go test ./internal/cerebro/evals/ -run 'ScheduleSweeper'`.
- **Depends on:** U9.

#### U11 — Drift alarm sweeper  (ref `2ef967c23`)
- **Files:** NEW `server/internal/cerebro/evals/drift_sweeper.go`, store additions in `evals/store.go` (`ListActiveEvals`, `LatestRunStatusForEval`, `PassRateByTargetVersion` + `VersionPassRate.Rate()`), `server/cmd/server/main.go` (`go NewDriftSweeper(...).Run(sweepCtx, 24*time.Hour)`; CEREBRO-PATCH `main-eval-drift-sweeper`), and refactor `publishInboxNew` into a package-level helper in `advisory.go` (shared with U5).
- **Decision:** daily (24h), no-op when disabled. `assess` two signals in order: (1) newest run `failed|error` → drift; (2) pass-rate regression: `PassRateByTargetVersion` grouped by `target_version` ordered by each version's most-recent run; if `len>=2 && rates[0].Rate() < rates[1].Rate()` → drift. Alert every owner/admin. Card `type="eval_drift"`, `severity="attention"`, details `{eval_id,eval_key,eval_title,reason,kind:"eval_drift"}`.
- **Tests:** `evals/drift_sweeper_test.go` (newest-failed alerts; pass-rate regression alerts; healthy = silent; disabled = no-op).
- **Verify:** `go test ./internal/cerebro/evals/ -run 'Drift'`.
- **Depends on:** U5 (shares `publishInboxNew`).

#### U12 — Only owner/admin may edit an eval  (ref `8e5a51b5f`)
- **Files:** `server/internal/cerebro/evals/handler.go` (`Update`: capture `actorID`, load `existing`, `canEditEval(member, memberOK, actorID, existing)` — workspace owner/admin always; else only `eval.CreatedByID == actorID`; else 403 `"only the eval owner or a workspace admin may edit this eval"`).
- **Tests:** `evals/handler_access_test.go` (owner edits ok; admin edits ok; stranger 403; creator edits ok).
- **Verify:** `go test ./internal/cerebro/evals/ -run 'EditAccess|CanEdit'`.

#### U13 — Only admins/granted may set a BLOCKING eval gate  (ref `8fca39674`)
- **Files:** NEW `server/migrations/9149_cerebro_set_blocking_gate_capability.up.sql` + `.down.sql` (RENUMBERED from 9148; widen `cerebro_group_capability_known` CHECK to add `'set_blocking_gate'`), `server/internal/cerebro/grouppermissions/permissions.go` (`CapabilitySetBlockingGate` const + `knownCapabilities` + `CanSetBlockingGate(viewer, workspaceID)` admins-short-circuit), `server/internal/cerebro/evals/handler.go` (`BlockingGateAuthorizer` iface + `WithBlockingGateAuthorizer`; in `CreateBinding`, when `input.Blocking && authorizer != nil` require member + `CanSetBlockingGate` else 403), `server/cmd/server/cerebro_eval_drift.go` (`evalBlockingGateAdapter{svc *grouppermissions.Service}`), `server/cmd/server/router.go` (`WithBlockingGateAuthorizer(evalBlockingGateAdapter{...})`; CEREBRO-PATCH `cerebro-evals-blocking-gate`).
- **Decision:** default deny by absence (a group has the capability only if a row exists); admins always pass. Advisory bindings unrestricted — only blocking bindings gated, and only when an authorizer is wired.
- **Tests:** `evals/handler_binding_test.go` (admin sets blocking ok; granted member ok; ungranted member 403; advisory binding always ok), `grouppermissions/permissions_test.go` (`CanSetBlockingGate` admin/granted/denied).
- **Verify:** `make migrate-up && go test ./internal/cerebro/evals/ ./internal/cerebro/grouppermissions/ -run 'BlockingGate|SetBlockingGate'`.

---

## Sequencing & dependencies

Stages (each stage = one focused build session; run `make check`-relevant subset per unit, full `make check` at each stage close):

1. **Stage 1 — Theme 1 (Hooks):** U1 → U2 → U3 → U4. Self-contained; hook engine + evals store + one frontend package.
2. **Stage 2 — Theme 2 (phases/advisory):** U5 → U6, U7 in parallel with U6, then U8 (the engine extension; do last, highest risk). U8 depends on U5.
3. **Stage 3 — Theme 3 (drift):** U9 → U10, U11 (both depend on U9/U5), U12, U13 in parallel. Migrations 9148/9149.
4. **Stage 4 — Integration:** full `make check` on fresh checkout, Playwright E2E for U4 + U7 surfaces, open `prod-ready`/`staging-only` PR → CI green → merge to `main`.

## Deferred to Implementation

- U6: exact monitor-step insertion point in `materialize_issue_loop.go` (new `Phase` bound to `DoneStatus` vs post-terminal hook) — resolve by reading the materializer.
- U8: whether to introduce a distinct advisory `StepStatus` or reuse `StepCompleted` + warning outcome — decide from `chain_store.go:29-35` + `chain_driver.go:205-218` during execution.
- Confirm the exact `EventInboxNew` / inbox-item constructor names on new main (`driftwatch/sweeper.go` is the model) before writing U5/U11 inbox writes.
- Confirm `cerebrodb.DBTX` vs `*pgxpool.Pool` expected by each new store method (main uses both in places).

## Rebuild risks

1. **Migration collisions** — 9147 is taken; use 9148 (schedule) + 9149 (blocking-gate). Verify `ls server/migrations | grep 91` before writing.
2. **Old loops engine is gone** — U6 (and only U6) must be re-expressed in the new chain/materialize engine, not the deleted `spec.go`/`compile.go`.
3. **Two eval-gate paths** — do not double-gate. The condition path (`check_passes`) is already phase-aware; the block-chain path is not (U8 fixes that). Ensure a single binding isn't evaluated by both for the same phase.
4. **Fail-closed regressions** — every new read defaults to blocked/false on no-run; advisory/drift/monitor never change an advance decision. Prove with tests.
5. **Flag discipline** — all OFF; CI must pass with flags OFF (features are dark). Drift additionally gated by `CEREBRO_EVAL_DRIFT_ENABLED`.

## Management summary (dansk, til ikke-tekniske læsere)

**Hvad:** De tre sidste dele af Evals v2 (prøver på automatiske triggere, "kun advar" + flere faser på arbejdsgange, og drift med tidsplan/alarmer/rettigheder) bygges om, fordi motoren bag arbejdsgange blev skrevet helt om, efter delene blev bygget første gang. **Hvorfor:** Den gamle byggeklods findes ikke mere, så delene kan ikke bare lægges sammen — de skal genopføres oven på den nye motor. **Pris:** Én til to bygge-omgange pr. tema (tre temaer), plus en samlet test- og udrulningsrunde. **Forventet resultat:** Alle tre funktioner virker på den nye motor, hver med sin egen automatiske test, alt bag en slukket kontakt indtil vi tænder pr. arbejdsområde. Færdig = alle automatiske tests grønne på en frisk kopi, og koden lagt sammen på test-miljøet.
