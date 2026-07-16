# Code Review Report

**Service**: daemon / project resources
**Review date**: 2026-07-16
**Reviewer**: Codex
**Branch**: `feat/local-directory-worktree-mode`
**PR**: multica-ai/multica#5513

## 1. Summary

- Reviewed the complete PR diff plus related execenv, Codex session, lock, GC, CLI, handler, and documentation paths.
- Initial findings: 8 substantive issues (High: 5, Medium: 3), all fixed.
- Final open findings: 0.
- Health score after fixes: **100/100**.

## 2. Findings and resolutions

### High

1. `server/internal/daemon/gc.go:641` — GC used `isActiveEnvRoot(wtPath)`, but active roots contain envRoot paths, never local worktree paths. A running task's worktree could be removed. Fixed by making GC take the same non-blocking per-issue lock used by task execution and holding it through status check and removal.
2. `server/internal/daemon/daemon.go:3109` / `server/internal/daemon/daemon.go:3943` — the lock gate and `runTask` independently re-probed Git. A source-path state change could select an issue lock and later fall back to the user's live tree. Fixed by making the gate's validated workdir decision authoritative and passing it through context.
3. `server/internal/daemon/local_worktree.go:75` / `server/internal/daemon/local_worktree.go:340` — the reuse fast path only checked for a `.git` file. A canonical directory belonging to another repo/branch, or a symlinked managed path, could be reused. Fixed with path-key validation, no-symlink containment checks, and exact common-dir plus branch validation.
4. `server/internal/daemon/daemon.go:3301` — all Git probe errors were treated as “not Git” and silently fell back to `in_place`, potentially granting access to the live checkout after Git execution, permission, cancellation, or corrupt-metadata failures. Fixed so only a definite non-repository result falls back; all operational failures fail the task.
5. `server/internal/daemon/gc.go:684` / `server/internal/daemon/local_worktree.go:187` — GC used `git worktree remove --force`, discarding uncommitted agent work and racing writes after its status check. Fixed by preserving dirty worktrees and removing without `--force`, letting Git perform the final atomic dirtiness check.

### Medium

1. `server/internal/daemon/local_worktree.go:93` — repository Git locks were keyed by the supplied path, so a repo root, subdirectory, and linked worktree of the same repository used different locks. Fixed by keying on canonical `git --git-common-dir`.
2. `server/internal/daemon/gc.go:673` — GC assumed the parent of `--git-common-dir` was the main working tree, which breaks separate-git-dir layouts; on failure it used raw `RemoveAll`, leaving metadata and risking data loss. Fixed by operating directly through `git --git-dir <commonDir>` and failing closed without raw deletion.
3. `server/internal/daemon/local_worktree.go:115` — branch recovery depended on localized stderr parsing and did not safely accept a concurrent process that won canonical worktree creation. Fixed with direct ref existence checks and exact repo/branch validation after collision.

## 3. Required documentation fixes

- Updated the built-in projects/resources skill and source map for the new CLI flag and behavior, as required by `CLAUDE.md`.
- Updated English, Chinese, Japanese, and Korean project-resource documentation so `in_place`, `worktree`, locking, persistence, and GC semantics are accurate.

## 4. Security and correctness checks

- No new database writes, tenant filters, authorization boundaries, SQL, or RPC metadata paths were introduced.
- Git commands use argument arrays and `--` path separators; no shell interpolation is used.
- Worktree paths reject traversal separators and symlink redirection below the managed root.
- Worktree and GC failures fail closed and are logged; no fallback writes to an unexpected directory remain.
- Lock order is consistently issue lock, then repository Git lock. No reverse-order path remains.

## 5. Verification

- `go test ./internal/daemon ./internal/handler`
- targeted `go test -race ./internal/daemon ...`
- `go vet ./internal/daemon ./internal/handler`
- `go test ./...` (all reviewed packages passed; the pre-existing, unchanged `internal/daemon/repocache` co-author hook test failed in this local environment and also failed three isolated reruns)
- `pnpm --filter @multica/docs typecheck`
- `git diff --check`

## 6. Conclusion

- [x] Passed
- [x] No open Critical / High / Medium / Low findings
- [x] Ready for maintainer review after the local changes are committed and pushed
