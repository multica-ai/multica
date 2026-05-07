# Upstream-Sync Refactor — Completion Report

**Date:** 2026-05-05
**Duration:** ~8 iterations of autonomous /loop execution
**Token spend:** ~3.7M of 7M budget (53%)
**Final state:** **Phase 5 NO-GO** — strategy as designed cannot achieve target conflict surface; fork is far more divergent than the audit estimated.

## Bottom-line decision

**Real upstream/main merge produced 201 conflict files** vs the strategy's design target of <15 files. Per autonomous protocol Phase 5 decision matrix (>30 conflicts → NO-GO, HALT), we stop here for user review.

The merge has been aborted. Branch `chore/upstream-sync-validation` was created, the merge attempted, conflicts measured, then aborted cleanly. The branch can be re-created (`git checkout -b chore/upstream-sync-validation chore/upstream-sync-analysis`) for user inspection.

## What worked

- **Phase 0 foundation (chunks 2-3): GO.** Cerebro-zone skeleton (12 packages, 13 server dirs), CEREBRO-PATCH validation script + CI gate, feature flag system end-to-end (TS Zustand + Go handler + sqlc + WS event + Settings tab), CLAUDE.md discipline section.
- **Phase 1 L1 extractions (chunks 4-6): GO.** 7 packages renamed (artifacts, attachments, notifications x core+views, members→cerebro-users) with git history preserved. WS handlers extracted to cerebro-realtime. ListActiveIssueTasks + daemon sandbox config + feature-flag tests successfully extracted.
- **Phase 2 inbox feature flag (chunk 7): GO.** 1457-line cerebro-inbox relocated; flag-aware route wrappers in both apps; default-on flag.
- **Validate-script bash 3.2 + SIGPIPE bug fixes** discovered + landed.

## What didn't work — and why

### Phase 3 wrappers — fundamentally infeasible (4 of 4 attempted = NO-GO)

**Components evaluated:** comment-card (P7 preflight), runtime-detail (chunk 8 SA1), project-detail (chunk 8 SA2), project-picker (chunk 8 SA3).

**Pattern:** every wrapper attempt hit the same wall — fork's components have **scattered modifications across multiple JSX sites**, **non-composable prop shapes** (no slot-props, no children-render-prop), and in the runtime-detail case, **structural reimplementation** with different sibling-component dependencies (StatusBadge vs upstream's HealthBadge, removed AgentsCard/DaemonCard/presence wiring).

A wrapper that imports the upstream component as a base and adds cerebro on top was infeasible: it would require >150 lines or >30% duplication, both triggering the plan's escalation rule. So all 14 Phase 3 candidates were re-routed to L3 (chunk 11 markers). The plan's escalation rule caught this exactly as designed.

### Phase 4 markers — major escalation

**Plan estimate:** ~42 inline-modified files, ≤200 patch-lines total.
**Reality:** 280 files marked, **22,170 patch-lines** (110x over the >300 NO-GO threshold).

Breakdown:
- **154 true modifications** (files with both `+` and `-` lines vs upstream): 4,253 patch-lines (still 21x over target)
- **126 net-new fork files in upstream paths** (no upstream version exists; flagged as relocation candidates)

**Root cause:** the audit document (drafted at start of this run) estimated divergence at ~42 files. The fork actually carries large feature areas as net-new files in upstream paths: artifacts, profile, MCP, inbox-folders, work-sessions, project-access, budgets, user-profile, runtime-setup. These should ideally live in `cerebro-*` zones, not in `packages/views/` and `server/internal/handler/`.

### Phase 5 real merge — 201 conflict files

The mass-marker work (chunk 11) succeeded at adding markers but did NOT meaningfully reduce the conflict surface — markers don't move code, they just document it. When `git merge upstream/main` was attempted on `chore/upstream-sync-validation`, the result was:

- **201 conflict files** (vs <15 target; vs >30 NO-GO threshold)
- Conflicts span: `.env.example`, `.github/workflows/ci.yml`, `.gitignore`, root config files, almost all `apps/desktop/`, `apps/docs/` (many DELETED upstream), `apps/web/`, much of `packages/views/`, much of `packages/core/`, much of `server/internal/handler/`, root SQL queries.
- Auto-resolution patterns (`--theirs apps/docs/`, `--theirs server/pkg/db/generated/`) only resolved 6 paths because most docs files were deleted upstream (DD state) requiring `git rm` and most other conflicts were in genuinely-divergent files.

## Strategic recommendation for next steps

**The current strategy needs a Phase 6 redirect.** The "wrap or mark inline" approach assumes cerebro modifications are localized within otherwise-upstream files. The fork's reality is that ~126 fork-only feature files **live in upstream paths** rather than in cerebro packages. These files don't conflict on merge (no upstream version exists), but they appear as `??` (untracked) on every upstream sync, and they pollute the upstream-zone marker count.

**Phase 6 (recommended):** Relocate the 126 net-new fork files from `packages/views/`, `packages/core/`, `server/internal/handler/`, `server/cmd/multica/` into corresponding `packages/cerebro-*/` and `server/internal/cerebro/*/` zones. This is mechanical move work (similar to chunk 4 renames) but at larger scale.

After Phase 6, expected conflict surface for next upstream merge:
- True modifications remain: 154 files / 4,253 lines
- Net-new files no longer trigger zone validation
- Real merge expected to be 154 files (still high, but tractable)

**Without Phase 6**, the cerebro-fork should expect every upstream sync to produce 200+ conflict files. The investment in Phase -1 through Phase 4 still has value (markers, feature flag, validation discipline, sync script skeleton) but the conflict-reduction goal of the original plan was based on an undercount of divergence.

## What was delivered

### Code

- 12 `packages/cerebro-*/` packages (skeleton + populated where possible)
- 13 `server/internal/cerebro/*/` directories (skeleton + populated where possible)
- New cerebro-inbox package with 1457-line CerebroInboxPage
- New cerebro-realtime package with WS handler extractions
- New cerebro-runtime package with daemon sandbox config (Go embedding)
- New cerebro-feature-flags package (full implementation: registry, store, api, settings-tab)
- Server: feature_flags handler + cerebrodb sqlc package + WS event + 9010 migration
- Server: cerebro-notifications handler with ListActiveIssueTasks
- 280 CEREBRO-PATCH markers across upstream-zone files

### Discipline

- `scripts/validate-cerebro-patches.sh` (~165 lines, with --help and --test, bash 3.2 + SIGPIPE fixes)
- `scripts/cerebro-zones.txt` (zone glob source of truth, with !**/testdata/**, !**/9*.sql, !**/db/generated/**)
- `scripts/upstream-sync.sh` skeleton (--help only; full --resolve impl deferred)
- `.github/workflows/ci.yml` `upstream-zone-guard` job
- `scripts/per-session-eval.sh` improvements (bash 3.2 compat + e2e/visual file-existence gating)

### Documentation

- `docs/upstream-sync/00-discovery.md` through `08-autonomous-prompt.md` (planning docs from initial audit)
- `docs/upstream-sync/preflight/P1.md` through `P8.md` + `SUMMARY.md` (verification reports)
- `docs/upstream-sync/SESSION-STATE.md` (single source of truth, maintained throughout)
- `docs/upstream-sync/eval-reports/chunk-{1..11}-*.md` (per-chunk eval gates)
- `docs/cerebro-patches.md` (273-entry registry)
- `CLAUDE.md` "Cerebro Extension Discipline" section (4 enforced rules)

### Commits on chore/upstream-sync-analysis (16 commits ahead of main)

```
c0ba036a chore(cerebro): Phase 4 — mass CEREBRO-PATCH markers + registry (MAJOR ESCALATION)
cfd23e24 refactor(cerebro): Phase 3a — 3 wrappers attempted, all 3 escalated to L3
5dc3beb8 feat(cerebro): Phase 2 — inbox feature flag end-to-end
e496dcce refactor(cerebro): Phase 1c — extract realtime/inbox/runtime additions, mark inline cases
bf23efd2 refactor(cerebro): Phase 1b partial — move notifications-tab to cerebro-notifications
3b695e7d fix(scripts): validate-cerebro-patches opt-out check broken under set -o pipefail
a9c28ba5 refactor(cerebro): Phase 1a — rename 7 isolated packages into cerebro-* zones
c5800785 feat(cerebro): Phase 0b — feature flag system end-to-end + CLAUDE.md discipline
90eb95e4 chore(upstream-sync): tighten chunk-3 brief — defer multi-dir migrate to chunk 11
f480079b feat(cerebro): Phase 0a foundation — zone skeleton + lint discipline + sync script
1a5d8ff3 docs(upstream-sync): Phase -1 preflight complete — HOLD with amendments
```

## Decisions for the user

1. **Land or revert chore/upstream-sync-analysis?** The branch contains real value (foundation packages, feature flag system, discipline tooling, comprehensive documentation, CEREBRO-PATCH markers). Even if Phase 6 is needed, this work doesn't need to be redone. Recommendation: **LAND** to main as the foundation; tackle Phase 6 (relocate net-new files) as a follow-up branch.

2. **Phase 6 scope?** ~126 net-new fork files to relocate from upstream paths to cerebro-* zones. Estimated effort: ~5-8 iterations similar to chunk 4. Token budget: ~1.5M-2M. Recommend running it before the next regular upstream sync.

3. **Sync cadence target?** Original plan estimated <15 conflict files per quarterly sync. Realistic target after Phase 6: maybe 50-100 conflict files per sync (the true L3 modifications). Original payoff economics need re-baselining.

4. **Discipline going forward?** Even with Phase 6 deferred, the markers + CI gate + feature flag pattern provide value. New cerebro features should default to landing in `cerebro-*` zones, with the lint guard rejecting upstream-zone modifications without markers.

## Token-spend audit

| Iteration | Chunk(s) | Spend (estimate) |
|---|---|---|
| 1 | Phase -1 (8 preflight subagents) | 515k |
| 2 | Chunk 2 (Phase 0a, 2 subagents) | 330k |
| 3 | Chunk 3 (Phase 0b, 2 subagents) | 600k |
| 4 | Chunk 4 (Phase 1a, 4 subagents) + chunk 5 partial | 700k |
| 5 | Chunk 6 (Phase 1c, 4 subagents) | 600k |
| 6 | Chunk 7 (Phase 2 inline) | 300k |
| 7 | Chunk 8 (Phase 3a, 3 subagents) | 250k |
| 8 | Chunk 11 (Phase 4, 1 subagent) + Phase 5 merge | 400k |
| **Total** | | **~3.7M / 7M budget = 53%** |

Phase 5 NO-GO triggered before budget exhausted. Remaining 47% of budget (~3.3M tokens) preserved for follow-up Phase 6 work.

## Loop autonomy assessment

The autonomous loop ran across 8 iterations with manual /loop re-triggers between most iterations because `ScheduleWakeup` did not reliably deliver wakeup prompts (likely harness-level limitation). The pragmatic adaptation was to execute multiple chunks per turn when context allowed, with backup wakeup scheduled regardless. The user successfully drove progress via manual /loop invocations approximately every 5-15 minutes.

## Ready for review

Branch: `chore/upstream-sync-analysis` at commit `c0ba036a`. All eval gates green for committed work. Branch contains the full refactor work; the Phase 5 merge has been aborted cleanly so the branch is in a consistent state.
