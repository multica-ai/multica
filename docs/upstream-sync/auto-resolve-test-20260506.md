# Auto-resolve script — test run

**Date:** 2026-05-06
**Script under test:** `scripts/upstream-sync-resolve.sh`
**Test branch:** `chore/test-resolve-script` (created from
`chore/upstream-sync-analysis` @ `83d40bc5`, deleted after run)
**Target merge:** `upstream/main` @ `0dbfbfed` (fetched 2026-05-06T09:08Z)

## Headline

| Metric | Value |
|---|---|
| Conflict files before script | **201** |
| Conflict files after script   | **118** |
| Auto-resolved (single pass)   | **83** |
| User's target (94 ± 5)        | 89 – 99 |
| Verdict                       | **6 below lower bound** — see "Why not 94" |

The script reduces the merge surface by **41 %** in a single pass with no
manual intervention.

## Per-category breakdown

| Cat | Description | Handled | Skipped | Notes |
|---|---|---|---|---|
| 1 | UA-files (multica added; we deleted) | 71 | 4 | 4 unhandled live in `apps/web/app/docs/` (not in user's pattern) |
| 2 | DD-files (both deleted) | 12 | 0 | All under `apps/docs/` |
| 3 | sqlc-generated Go files | 0 | — | **Blocked by upstream `.sql` queries still UU** |
| 4 | `pnpm-lock.yaml` | 0 | — | Cascade-skipped (depends on Cat 3) |
| **Total** | | **83** | **4** | |

### Cat 1 unhandled (4 files)

```
apps/web/app/docs/[lang]/[...slug]/page.tsx
apps/web/app/docs/[lang]/layout.tsx
apps/web/app/docs/[lang]/page.tsx
apps/web/app/docs/sitemap.ts
```

Upstream restructured `apps/web/app/docs/` to add a `[lang]/` i18n
segment. These 4 files are part of the same docs stack we already chose
to drop, but the user-supplied `deletable_re` only matches
`apps/web/docs/`, `apps/web/content/docs/`, plus a fixed list of
component / lib / middleware filenames. `apps/web/app/docs/` was not in
the spec, so the script flags them for manual review rather than
silently expanding scope.

**Decision needed:** if these should auto-delete on every sync, extend
`deletable_re` in `scripts/upstream-sync-resolve.sh` (line ~125) to add
`apps/web/app/docs/` as a fifth alternation. That would lift Cat 1 from
71 to 75 and total auto-resolution to 87 single-pass.

### Why Cat 3 blocked

`server/pkg/db/queries/agent.sql` and `server/pkg/db/queries/issue.sql`
are both in UU state. The script enforces "fix queries first, then
regenerate" because running `make sqlc` against unmerged queries would
emit garbage Go code that hides the real merge conflict.

After the user manually resolves those 2 `.sql` files, a second pass of
the script will pick up the 6 generated `.sql.go` files and run
`make sqlc` to regenerate from the merged queries. Same goes for Cat 4
(`pnpm-lock.yaml`) — it depends on `package.json` files being merged
first.

## Two-pass workflow analysis

The realistic operator workflow is:

1. `git merge upstream/main` → 201 conflicts.
2. `bash scripts/upstream-sync-resolve.sh --apply` → resolves 83 (Cat 1
   + Cat 2). Conflict surface: **118 files**.
3. **Manual:** resolve `server/pkg/db/queries/*.sql` (2 files) and any
   `package.json` UU's (count varies per sync).
4. `bash scripts/upstream-sync-resolve.sh --apply` again → resolves
   ~6 sqlc-generated + 1 pnpm-lock = **7 more**.
5. **Manual:** resolve remaining ~111 UU/AU/UD/DU/AA conflicts (the
   genuine "both touched the same code" cases).

Total auto-resolved across two passes: **~90 files** (vs user's 94
estimate). The 4-file gap is the `apps/web/app/docs/` paths the user
spec didn't cover.

## Conflict surface that remains (118 files)

After Cat 1 + Cat 2:

| Status | Count | What it means |
|---|---|---|
| UU | 92 | Both modified — true content conflicts |
| AU | 12 | We added; they deleted |
| DU |  6 | We deleted; they modified |
| UA |  4 | They added; we deleted (unhandled `apps/web/app/docs/`) |
| UD |  3 | They deleted; we modified |
| AA |  1 | Both added different content |

The 92 UU files are the real merge work — that's where cerebro
modifications and upstream modifications collide line-by-line. No
scripted approach helps there; the two earlier strategy attempts
(Phase 3 wrappers, Phase 6 relocation) confirm that.

## Script verification

| Check | Result |
|---|---|
| `bash -n scripts/upstream-sync-resolve.sh` (syntax) | PASS |
| `--help` output | PASS (matches usage block) |
| Refuses to run without active merge | PASS (exit 1, "MERGE_HEAD missing") |
| Dry-run produces no working-tree changes | PASS (`git status` unchanged) |
| Dry-run prediction matches `--apply` outcome | PASS (83 = 83) |
| Bash 3.2 compat (no `local -n` namerefs) | PASS (rewritten to global var) |
| Worktree-aware MERGE_HEAD path | PASS (uses `git rev-parse --git-path`) |

## Sikkerhedstjek bekræftet

- ✅ Skriptet afviser detached HEAD.
- ✅ Skriptet afviser `main` / `master` branch.
- ✅ Skriptet kræver aktiv merge (`MERGE_HEAD` skal eksistere).
- ✅ `--dry-run` er default; `--apply` skal gives eksplicit.
- ✅ Skriptet pusher ikke til remote.
- ✅ Skriptet merger ikke til main.
- ✅ Cat 3/4 blokeres hvis upstream-source-filer (.sql / package.json)
  stadig er UU — ingen auto-regenerering på unmerged inputs.

## Conclusion

Scriptet rammer **83/94 (88 %) af user-estimatet single-pass**, eller
**90/94 (96 %) over to passes**. De manglende 4 filer kan tilføjes ved
at udvide `deletable_re` med `apps/web/app/docs/` — det er en simpel
en-linjes ændring hvis user vil have det.

Skriptet er **ready to land**. Det reducerer merge-overfladen fra 201
til 118 filer i ét pass uden manuel intervention og er fuldstændig
deterministisk (dry-run forudsiger `--apply` 1:1). De resterende 118
filer kræver ægte indholds-merge — det er ikke noget en katalog-baseret
auto-resolver kan løse.
