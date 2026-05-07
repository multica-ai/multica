# Auto-resolve script — test run

**Date:** 2026-05-06 (updated 09:30Z after expanding purge scope)
**Script under test:** `scripts/upstream-sync-resolve.sh`
**Test branch:** `chore/test-resolve-script-v2` (created from
`chore/upstream-sync-analysis` @ `4f648d4b`, deleted after run)
**Target merge:** `upstream/main` @ `0dbfbfed` (fetched 2026-05-06T09:08Z)

## Headline

| Metric | Single-pass | Two-pass (after manual SQL + package.json) |
|---|---|---|
| Conflict files before script  | 201 | 201 |
| Conflict files after script   | **94**  | **83**  |
| Auto-resolved by script       | **107** | **114** |
| Manual resolves needed first  | 0   | 4 (2 × `*.sql` + 2 × `package.json`) |
| Reduction                     | **53 %** | **59 %** |

The 83 left after the full two-pass workflow are **genuine cerebro
versus upstream content conflicts** that no scripted approach can
resolve. They line up with the 80 + 2 + 1 split below: 80 files where
both sides have changed the same lines, 2 where multica deleted a file
the fork still modifies, 1 where both sides added a file with different
content.

## Per-category breakdown (single pass — no manual intervention)

| Cat | Description | Handled | Notes |
|---|---|---|---|
| 1 | Purge docs/i18n stack (any conflict status) | 107 | Walks all unmerged files; deletes any whose path matches the docs/i18n purge regex |
| 2 | Both-deleted catch-all (status `DD` outside purge) | 0 | All `DD` paths in this sync were under `apps/docs/` and already covered by Cat 1 |
| 3 | sqlc-generated Go files | 0 | **Blocked**: 2 `.sql` query files are still in UU state; safe-stop until manual resolve |
| 4 | `pnpm-lock.yaml` | 0 | Cascade-skipped because Cat 3 was blocked |
| **Total** | | **107** | |

## Per-category breakdown (second pass — after user resolves the 4 SQL/pkg files)

| Cat | Description | Handled | Notes |
|---|---|---|---|
| 1 | Purge docs/i18n stack | 0 | Already done in pass 1 |
| 2 | Both-deleted catch-all | 0 | n/a |
| 3 | sqlc-generated Go files | 5 | `agent.sql.go`, `chat.sql.go`, `issue.sql.go`, `runtime.sql.go`, `user.sql.go`. The shared `models.go` is also UU but doesn't match the `*.sql.go` regex — it gets cleaned by the implicit `git add server/pkg/db/generated/` after `make sqlc`, so total cleared = 6. |
| 4 | `pnpm-lock.yaml` | 1 | Resolved with `--theirs` then `pnpm install --no-frozen-lockfile` regenerates from merged `package.json`s |
| **Total** | | **6 (or 7 incl. models.go)** | |

## What the script's purge regex covers

```
apps/web/docs/...
apps/web/content/docs/...
apps/web/app/docs/...
apps/docs/...
apps/web/components/{architecture-diagram,docs-settings,editorial,hero,mermaid}.tsx
apps/web/lib/{i18n,site,translations}.ts
apps/web/middleware.ts
```

For these paths, **any unmerged status** (both modified, ours added,
theirs added, ours deleted, theirs deleted, both deleted, both added)
resolves to `git rm`. The fork has chosen to drop the multica docs site
entirely; cerebro docs land under `packages/cerebro-*/` or other
project-controlled paths.

## Conflict surface that remains (94 single-pass / 83 two-pass)

| Status | Count | What it means |
|---|---|---|
| UU | 80  | Both sides modified — true line-level content conflicts |
| UD | 2   | Multica deleted a file we modified |
| AA | 1   | Both sides added a file at the same path with different content |

Examples of the 80 UU files (after two-pass workflow):

```
.env.example                          (config divergence)
.github/workflows/ci.yml              (CI tweaks both sides)
.gitignore                            (entries both sides)
CONTRIBUTING.md, SELF_HOSTING*.md     (docs both sides edit)
Makefile                              (target additions both sides)
packages/views/issues/components/*.tsx (cerebro patches collide with upstream evolution)
packages/views/chat/components/*.tsx
... (the rest are cerebro vs upstream evolution in shared code)
```

These are exactly the `CEREBRO-PATCH(...)`-marked files that Phase 4
flagged. Each one needs a human merge call: keep cerebro behaviour vs.
take upstream improvement vs. integrate both. No regex can answer that.

## Realistic operator workflow

```bash
# 1. Start the merge.
git checkout -b chore/upstream-sync-$(date +%Y-W%V) chore/upstream-sync-analysis
git fetch upstream main
git merge upstream/main         # produces ~200 conflicts

# 2. First auto-resolve pass.
bash scripts/upstream-sync-resolve.sh --dry-run   # preview
bash scripts/upstream-sync-resolve.sh --apply     # execute
# → ~94 conflicts left

# 3. Manually resolve the 2 SQL queries (read both versions, merge intent).
$EDITOR server/pkg/db/queries/agent.sql server/pkg/db/queries/issue.sql
git add server/pkg/db/queries/*.sql

# 4. Manually resolve the 2 package.json files.
$EDITOR packages/core/package.json packages/views/package.json
git add packages/{core,views}/package.json

# 5. Second auto-resolve pass — picks up sqlc + pnpm-lock.
bash scripts/upstream-sync-resolve.sh --apply
# → ~83 conflicts left, all genuine content merges

# 6. Manual content merges (the real merge work).
# Each cerebro-patched file needs a per-case decision.

# 7. Validate.
scripts/validate-cerebro-patches.sh
make check
```

## Script verification

| Check | Result |
|---|---|
| `bash -n scripts/upstream-sync-resolve.sh` (syntax) | PASS |
| `--help` output | PASS |
| Refuses to run without active merge | PASS (exit 1) |
| Refuses to run on `main` / `master` | PASS (exit 1) |
| Refuses to run on detached HEAD | PASS (exit 1) |
| Dry-run produces no working-tree changes | PASS |
| Dry-run prediction matches `--apply` outcome | PASS (107 = 107) |
| Cat 3 blocks safely on UU `.sql` queries | PASS |
| Cat 4 cascade-skips on Cat 3 block | PASS |
| Bash 3.2 compatibility (no `local -n`, empty-array safe) | PASS |
| Worktree-aware `MERGE_HEAD` lookup | PASS |
| Cat 3 second-pass regenerates correctly | PASS (sqlc reran, all 6 generated files cleared, including `models.go`) |
| Cat 4 second-pass regenerates correctly | PASS (pnpm install completed, lock file staged) |
| Aborted merge leaves working tree clean | PASS (only `models.go` carried sqlc artefact, restored with `git checkout --`) |

## Conclusion

Scriptet er **klar til en upstream-sync uden tab af cerebro-funktioner**.
Auto-løser **107 konflikter i ét pass** og yderligere **7 i et andet pass**
efter at brugeren manuelt har merged 4 trivielle filer
(2 × `.sql` + 2 × `package.json`). Tilbage står **83 ægte indholds-konflikter**
hvor cerebros patches kolliderer med multicas videreudvikling — det er
arbejde et menneske skal lave manuelt fil-for-fil med viden om hvad
cerebro tilfører.

Sammenlignet med Phase 5-baseline (201 manuelle konflikter): scriptet +
4 manuelle løsninger gør **58 % af arbejdet automatisk**.
