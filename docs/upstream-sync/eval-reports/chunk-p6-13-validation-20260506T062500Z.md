# Phase 6 — Done-criterion validation

**Timestamp:** 2026-05-06T06:25:00Z
**Branch tested:** `chore/upstream-sync-analysis` (post-chunk-6 commit `beb3fa90`)
**Test merge target:** `upstream/main` (commit `44a0ced5`, fetched 2026-05-06)

## Headline numbers

| Metric | Phase 5 (pre-Phase-6) | Phase 6 (post-moves) | Delta |
|---|---|---|---|
| Net-new fork files in upstream zones | 116 | 79 | **-37** |
| Conflict files on `git merge upstream/main` | 201 | **201** | **0** |
| Phase 6 done-criterion (`<50` conflict files) | n/a | **NOT MET** | n/a |

## What moved (37 files, 7 commits)

| Commit | Chunk | Package | Files |
|---|---|---|---|
| `1f28e85e` | 1 | cerebro-profile (NEW) | 13 |
| `c637c258` | 2 | cerebro-chat (NEW) | 2 |
| `3d21a7e7` | 3 | cerebro-access (frontend) | 6 |
| `3d907464` | 11 | cerebro-preferences (NEW) + cerebro-attachments + cerebro-access + cerebro-ui populated | 9 |
| `8bb4aa97` | 10 | cerebro-inbox (frontend) | 2 |
| `7229a893` | 4 | cerebro-budgets (NEW) | 2 |
| `beb3fa90` | 6 | cerebro-runtime (frontend) | 3 |

Each chunk landed with `bash scripts/per-session-eval.sh` green
(typecheck + unit-tests-ts + go-tests + cerebro-patches all PASS).

## What didn't move (~79 files) and why

**Backend `*Handler` method files (chunks 3, 4, 5, 8, 9, 10 backend portions):**
- `server/internal/handler/{access,artifact,artifact_folder,artifact_upload,budget,budget_preclaim,inbox_folder,member_detail,notifications,project_access,runtime_setup,user_preferences,user_profile,work_session,workspace_pause,install_runtime,install_runtime_embed}*.go`
  define methods on the upstream `*Handler` struct. Their tests call
  unexported methods directly via the package-level `testHandler`,
  `testPool`, etc. spun up by `handler_test.go` `TestMain`.
- Moving any of them out of `package handler` requires:
  1. Renaming methods (lowercase → exported) and relocating to a new
     receiver type.
  2. Re-implementing the DB+Hub+Bus+EmailService fixture per cerebro
     test-package.
  3. Updating every call site in upstream's `*Handler` methods that
     currently invokes `h.canAccessProject(...)` etc.
- That is a real refactor far outside Phase 6's mechanical-move scope.
  Decision: stay-with-marker; track for revisit when an upstream sync
  forces relocation.

**`package main` cobra commands (chunk 7 — cerebro-mcp CLI):**
- Attempted: relocate 7 `cmd_mcp*.go` files to
  `server/internal/cerebro/mcp/cli/` and export `McpCmd`.
- Build failed: the files reference unexported helpers
  `resolveProfile`, `resolveToken`, `resolveServerURL`,
  `resolveWorkspaceID` (defined in `cmd_agent.go` / `cmd_auth.go`) and
  the linker-set `version` var. Those four helpers are called in 15+
  call sites across other `package main` files.
- Extracting them to a new shared `internal/cli/flagresolve` package
  (the only way to break the cycle) is its own substantial refactor.
- Decision: REVERTED chunk 7 cleanly; document the blocker.

**Type cycle (chunk 5 — `packages/core/types/artifact.ts`):**
- The 14 Artifact types are re-exported from
  `packages/core/types/index.ts` (upstream-zone file with
  `CEREBRO-PATCH(core-types-index)`).
- Moving `artifact.ts` would force `packages/core/types/index.ts` to
  import from `@multica/cerebro-artifacts`, which violates the rule
  "core MUST NOT depend on cerebro-*". The alternative — removing the
  re-export and migrating ~6 consumers — is a wider blast radius than
  Phase 6's scope.
- Decision: leave artifact.ts in core/types/; flag for follow-up.

**Test files of upstream code with low payoff:**
- `packages/views/issues/components/comment-card.test.ts` and
  `packages/views/editor/extensions/submit-shortcut.test.ts` test
  upstream code via relative imports. Moving them requires adding new
  views subpath exports and rewiring vi.mock targets — risk of mock
  resolution breakage outweighs the cleanup payoff for already-
  validator-excluded files.
- Decision: defer.

## Why moving 37 files didn't reduce conflict count

**Empirical finding:** Phase 6 plan assumed that net-new fork files in
upstream paths contributed to merge conflicts. They do not — `git
merge` only conflicts on AA (both-added with different content), MM
(both-modified), or DM/MD states. A file that exists in fork but not
upstream stays as a clean ADD on the merge result.

The 201 conflicts that Phase 5 measured come almost entirely from:
1. Upstream-modified files that fork ALSO modified
   (`packages/views/chat/components/chat-message-list.tsx`,
   `packages/views/issues/components/comment-card.tsx`,
   `packages/views/issues/components/issue-detail.tsx`,
   `packages/views/projects/components/projects-page.tsx`,
   etc. — all the cerebro-patched upstream files).
2. Upstream-modified config / SQL files that fork also touched
   (`packages/views/package.json`, sqlc-generated code,
   `server/pkg/db/queries/*.sql`).
3. Files upstream deleted that fork still has (DM state — common in
   `apps/docs/`, deprecated handlers).

Moving net-new fork files OUT of upstream paths only changes the
"untracked additions" count — not the conflict count.

## Recommended next step

**Phase 6's <50 conflict-files target was based on an incorrect model
of where conflicts come from.** To actually reduce conflicts, the work
needed is one of:

A. **Wrap or replace the 50-100 cerebro-patched upstream files with
   thin wrappers + a parallel cerebro-implementation.** Each wrapper
   re-exports the upstream component from cerebro-* and lets fork
   modifications stay in cerebro-* code paths. Phase 3 of the original
   plan tried this and concluded it was infeasible (4 of 4 attempted
   wrappers escalated to L3).

B. **Accept the 200-conflict baseline as the realistic upstream-sync
   cost** and focus on tooling that makes resolving 200 conflicts
   tractable — better `scripts/upstream-sync.sh --resolve` patterns,
   per-file conflict templates, semantic merge tooling. The validator
   + CEREBRO-PATCH discipline (chunks 0-4 of Phase 1-4) already keeps
   future divergence bounded.

C. **Branch fork off a frozen upstream version and re-baseline.** If
   upstream divergence is structural rather than additive, periodic
   re-merges may cost less than treating upstream as a continuously
   trackable target.

The 37 files Phase 6 did move are still real wins — they live in
cerebro packages now, won't churn future merge stats, and isolate
cerebro logic for downstream contributors.

## Decision matrix per autonomous-loop protocol

```yaml
done_criterion: <50 conflict files
measured: 201 conflict files
verdict: NOT MET
go_no_go: NO-GO
recommendation: stop loop; user review needed for next-step direction
```

**Loop stopped per user instruction.** Branch `chore/upstream-sync-analysis`
contains the 7 Phase 6 commits (`1f28e85e..beb3fa90`) plus the chunk
1-12 doc trails. Each commit passes its own per-session-eval gate.
Branch is in a clean, mergeable state for review and (when user
confirms) landing to main.
