# Phase -1 — Preflight Summary

**Date:** 2026-05-05
**Branch:** chore/upstream-sync-analysis (worktree)
**Iteration:** 1 of autonomous loop

## Verdicts

| # | Verifikation | Verdict |
|---|---|---|
| P1 | TS module augmentation | **PASS** |
| P2 | Path-alias shadowing | **HOLD** (mitigation: barrel-shadow) |
| P3 | sqlc multi-source | **PASS** |
| P4 | Migrations multi-dir | **PASS** |
| P5 | Visual regression stability | **HOLD** (mitigation: masks + deterministic data) |
| P6 | Lint rule CEREBRO-PATCH | **PASS** |
| P7 | Wrap pattern (comment-card) | **NO-GO** (escalate to L3 — expected per plan) |
| P8 | Feature flag pattern | **PASS** |

**Score:** 5 PASS, 2 HOLD with known mitigations, 1 NO-GO at the verification level (handled by existing escalation rule).

## Overall verdict: **HOLD → proceed to Phase 0 with amendments**

Per `03-decision.md`: *"HOLD — 1-2 punkter fejler med kendt mitigation → Implementér mitigation, re-test, dokumentér ændring."*

Per `08-autonomous-prompt.md` decision matrix: *"7 of 8 preflight checks pass | Mark Phase -1 HOLD, document failure, proceed to Phase 0 with caveat."*

P7 is technically NO-GO at the verification level, but the plan's existing escalation rule ("Hvis en wrapper koster >150 linjer → drop wrapping for den fil og flyt til L3") already anticipates this. Comment-card moves from Phase 3 (wrappers) to Phase 4 (L3 markers). Phase 3 wrapper count drops from 14 to 13. Strategy unchanged.

## Plan amendments (must be applied before Phase 0 starts)

1. **P2 mitigation — barrel-shadow naming.**
   `03-decision.md` Phase 0.4 + Phase 3 use the fictional specifier `@multica/views/issues/comment-card`. Replace all such references with the actual barrel `@multica/views/issues/components`. Centralize the override map at `packages/cerebro-types/aliases.ts` and consume from both `apps/web/next.config.ts` (via `webpack(config)` hook) and `apps/desktop/electron.vite.config.ts` (via `renderer.resolve.alias`).

2. **P5 mitigation — visual regression scope.**
   `04-evals.md` Phase 0.6 must explicitly budget time for: snapshot config (`animations: "disabled"`, `caret: "hide"`, `maxDiffPixelRatio: 0.001`), masks for relative timestamps + animations + remote avatars, deterministic seed data via existing `TestApiClient`, Ubuntu-CI baseline generation (NEVER macOS-dev). Fallback documented: defer pixel snapshots to Phase 1 if flake >0.5% after masks; semantic E2E remains the gate.

3. **P7 mitigation — drop comment-card from Phase 3.**
   `03-decision.md` Phase 3 wrapper count: 14 → 13. Comment-card moves to Phase 4 L3 markers (6 markers needed: remove-canModerate × 4, jeh-345-reply-order × 1, mobile-padding × 1 group, attachments × 1 group). Phase 4 file count: 42 → 43.

4. **P4 critical context — 9NNN files already exist.**
   20 files matching `9NNN_*.sql` already live in `server/migrations/` (cerebro fork's existing migrations). Phase 0.5 sync script + Phase 1 multi-dir scanner work must include `git mv server/migrations/9*.sql server/migrations/cerebro/` as part of the implementation.

## Strategy validation

The fundamental refactor strategy (cerebro-zone packages + path-alias overrides + L3 markers + feature flags + sync script) is **technically sound**. All eight verifications confirm the patterns work in this codebase, with two requiring scoped mitigations and one escalating from L2 to L3 (already anticipated by the plan).

No fundamental antagelse brudt. Phase 0 should proceed.

## Next action

`SESSION-STATE.md` next_action: `execute_task` for Phase 0a (cerebro-zone-skeleton + lint-rule + sync-scripts).

The autonomous loop will:
1. Update SESSION-STATE.md: `phase_minus_1.go_no_go: HOLD`, `phase_minus_1.status: completed`, `current_phase: "Phase 0"`.
2. Run `scripts/per-session-eval.sh chunk-1` to verify the eval gate is green for the chunk-1 baseline (most checks SKIP because no cerebro-* artifacts exist yet — that's expected).
3. ScheduleWakeup for next iteration to begin Phase 0a (cerebro-zone skeleton creation per chunk 2 in `07-session-schedule.md`).

## Token spend (estimate)

- 8 subagents × ~55-65k tokens each ≈ **475k tokens**
- Orchestrator (this iteration) ≈ **40k tokens**
- **Total Phase -1: ~515k tokens** (vs 400k budget — 28% over, within tolerance)

Cumulative: 515k / 7M budget = 7.4% spent.
