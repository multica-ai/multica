---
title: "How to safely upgrade a self-hosted Multica instance with local customizations to a new upstream release"
date: 2026-07-10
category: "workflow-issues"
module: "git"
problem_type: "workflow_issue"
component: "development_workflow"
severity: "medium"
applies_when:
  - "Self-hosted Multica instance with local customization commits on main"
  - "Upstream releases a new version with breaking API changes (e.g. ActorAvatar refactor)"
  - "Local customizations overlap with upstream feature areas (super-admin, skills, UI components)"
  - "PM2 manages both backend and frontend processes requiring restart after upgrade"
  - "Post-merge dependency mismatches need manual resolution (pnpm install)"
tags: [upstream-upgrade, self-hosted, local-customizations, merge-conflicts, post-merge, pm2, go-frontend, dependency-install]
---

# How to safely upgrade a self-hosted Multica instance with local customizations to a new upstream release

## Context

Self-hosted instances of Multica accumulate local customizations — feature additions, admin tooling, config struct fields, i18n tweaks — that sit on top of the upstream `main`. When upstream releases a new version with breaking changes (such as the v0.3.42 ActorAvatar refactor across 35 commits), the upgrade is not a simple `git pull`. The instance had 43 local customization commits spanning TypeScript UI code, Go backend config, SQL migrations, and test suites. A naive merge would produce dozens of conflicts and, if resolved carelessly, silently drop local work or introduce type errors from mismatched upstream APIs.

This document captures the full upgrade workflow as a repeatable practice, distilling the concrete steps, conflict-resolution strategies, and post-merge adaptation patterns into guidance for future upgrades.

## Guidance

Follow this sequence for every upstream release merge into a customized self-hosted instance. Each step has a specific purpose and must not be skipped.

### Step 1: Create a safety branch

Preserve the current state of `main` so you can always revert to it.

```bash
git branch main-backup-v0.3.42 main
```

This is your emergency rollback. If the merge goes badly, you can `git reset --hard main-backup-v0.3.42` and be exactly where you started.

### Step 2: Stash uncommitted local modifications

Uncommitted changes will block `git merge`. Stash them before starting.

```bash
git stash push -m "pre-upgrade"
```

This typically includes files like `AGENTS.md` with local agent instructions, `.env` overrides, or other working-tree modifications that are not yet committed.

### Step 3: Preview conflicts without committing

Attempt the merge with `--no-commit --no-ff` to see all conflicts without finalizing anything.

```bash
# Fetch latest upstream
git fetch origin

# Attempt merge without committing
git merge origin/main --no-commit --no-ff
```

If the conflicts look too severe or you are not ready, abort cleanly and restore your stash:

```bash
git merge --abort
git stash pop
```

If the conflicts look manageable, list them to understand the scope:

```bash
# List all conflicting files
git diff --name-only --diff-filter=U

# Count total conflicts
git diff --name-only --diff-filter=U | wc -l
```

### Step 4: Resolve conflicts using per-file strategies

Not all conflicts deserve the same resolution approach. Categorize each conflicted file and apply the appropriate strategy.

**Strategy A — Accept upstream for refactored APIs.** When upstream refactored a component you also customized, accept their version and adapt your local code afterward. Do not try to manually merge old size tokens with new ones.

```bash
# Accept upstream version for refactored files
git checkout origin/main -- packages/ui/actor-avatar.tsx
git add packages/ui/actor-avatar.tsx
```

Then update your local features that depend on the old API (see Step 5).

**Strategy B — Keep both for independent customizations.** When local changes and upstream changes touch completely different parts of the same file, combine them.

For example, a Go `Config` struct where you added `SuperAdminEmails` and upstream added LLM-related fields: both sets of fields are independent and should coexist.

```go
// After resolution — both local and upstream additions present
type Config struct {
    SuperAdminEmails []string // local customization
    LLMAPIKey        string   // upstream addition
    LLMBaseURL       string   // upstream addition
    LLMDefaultModel  string   // upstream addition
}
```

**Strategy C — Merge carefully for mixed files.** When both sides touched overlapping logic, read both sides and merge deliberately. Test suites are a common case: keep both test suites.

For `readonly-content.test.tsx`, the merge kept upstream's refactored test structure and appended the local admin-specific test cases. For `workspace.sql`, accept upstream's `DELETE` syntax improvement while preserving local admin queries.

### Step 5: Adapt local features to upstream API changes

After resolving conflicts, local features that depended on refactored upstream APIs must be updated. This is critical: **adapt your local code to the new upstream API, not the other way around.**

Concrete patterns from the v0.3.42 upgrade:

```tsx
// BEFORE: numeric size prop (old upstream API)
<AvatarChip size={14} />

// AFTER: token-based size prop (new upstream API, ActorAvatar refactor)
<AvatarChip size="xs" />
```

```tsx
// BEFORE: rounded-square avatar shape
<Avatar shape="rounded-square" />

// AFTER: all avatars are circles per upstream MUL-4277
<Avatar shape="circle" />
```

Update test assertions to match the new behavior. For example, snapshot tests will differ because the rendered avatar DOM changed shape.

### Step 6: Install dependencies

After merge, run dependency installation. Upstream may have added packages to `package.json` that are not automatically installed.

```bash
pnpm install
```

Do not skip this. Missing dependencies cause build failures that are confusing to diagnose. See `docs/solutions/workflow-issues/pnpm-install-after-upstream-merge.md` for the detailed explanation.

### Step 7: Full verification

Run every verification step in order. Do not stop at the first pass.

```bash
# Backend: Go compiles cleanly
cd server && go build ./... && cd ..

# TypeScript: no type errors introduced by the merge
pnpm typecheck

# Unit and integration tests pass
pnpm test

# Frontend builds successfully
pnpm build
```

If any step fails, fix the issue before proceeding. The typecheck step will catch cases where local features still reference removed upstream APIs.

### Step 8: Restart services and verify

```bash
pm2 restart multica-backend multica-frontend

# Verify services are responding
curl -s -o /dev/null -w "%{http_code}" http://localhost:3001  # frontend
curl -s -o /dev/null -w "%{http_code}" http://localhost:8081  # backend API
```

Both endpoints should return 200 (or 301/302 for frontend routing). If either fails, check pm2 logs for the specific process.

### Step 9: Restore uncommitted modifications

```bash
git stash pop
```

Resolve any minor conflicts between the merge result and your stashed changes. These are usually trivial since the stash contains non-overlapping working-tree edits.

### Step 10: Write a descriptive merge commit message

Document the resolution strategy in the commit message so future maintainers understand what happened.

```bash
git commit -m "$(cat <<'EOF'
merge(upstream): upgrade to v0.3.42 with local customization preservation

Conflicts resolved:
- actor-avatar.tsx: accepted upstream refactor (size tokens, all-circles),
  adapted local avatar-chip and R2-squad to new API
- Config struct: kept both local SuperAdminEmails and upstream LLM fields
- readonly-content.test.tsx: combined both test suites
- workspace.sql: accepted upstream DELETE syntax, kept admin queries

Post-merge: pnpm install, typecheck, tests, build all pass.
EOF
)"
```

## Why This Matters

A self-hosted instance with local customizations has no automated upgrade path. Every upstream release merge is a manual operation that requires judgment. Without a structured workflow:

- **Backups prevent catastrophe.** A `git branch` backup costs nothing but saves you from an irreversible bad merge.
- **Conflict preview prevents blind commits.** Running `--no-commit --no-ff` first lets you assess scope before making irreversible changes. If the conflict set is overwhelming, you can abort and prepare.
- **Per-file strategy prevents over-merging.** Treating every conflict the same leads to either dropping local customizations (accept upstream everywhere) or retaining stale API patterns (keep local everywhere). Matching the strategy to the file's situation preserves both local work and upstream improvements.
- **Adaptation direction matters.** Updating local code to match the new upstream API is forward-compatible. Modifying upstream code to match the old local API creates a fork that gets harder to maintain with each release.
- **Verification catches silent breakage.** A merge that compiles but fails tests still has defects. The full verification chain catches type mismatches, behavioral changes, and missing dependencies before they reach production.

## When to Apply

- Merging a new upstream release into a self-hosted Multica instance that has local customization commits on `main`.
- Any git repository where a fork has diverged significantly from upstream and needs periodic sync.
- Situations where `git merge` produces conflicts spanning both API-layer refactors and independent feature additions.
- Any upgrade where breaking API changes in upstream affect local feature code.

## Examples

### Complete upgrade from v0.3.41 to v0.3.42

Starting state: 43 local commits on `main`, 35 upstream commits in `origin/main` since last sync.

```bash
# 1. Backup
git branch main-backup-v0.3.42 main

# 2. Stash local modifications
git stash push -m "pre-upgrade"

# 3. Fetch and preview
git fetch origin
git merge origin/main --no-commit --no-ff

# 4. List and assess conflicts
git diff --name-only --diff-filter=U
# → packages/ui/actor-avatar.tsx, server/cmd/server/router.go,
#   packages/views/editor/readonly-content.test.tsx, ...

# 5. Resolve per-file (repeat for each conflict)
git checkout origin/main -- packages/ui/actor-avatar.tsx && git add packages/ui/actor-avatar.tsx
# ... manually resolve Config struct, test files, SQL files ...

# 6. Adapt local features
# Update AvatarChip size={14} -> size="xs"
# Update Avatar shape="rounded-square" -> shape="circle"

# 7. Install + verify
pnpm install
cd server && go build ./... && cd ..
pnpm typecheck
pnpm test
pnpm build

# 8. Commit
git commit -m "merge(upstream): upgrade to v0.3.42 ..."

# 9. Restart and verify service health
pm2 restart multica-backend multica-frontend
curl -s -o /dev/null -w "%{http_code}" http://localhost:3001
curl -s -o /dev/null -w "%{http_code}" http://localhost:8081

# 10. Restore stash
git stash pop
```

### Abort and retry when conflicts are overwhelming

If the initial `--no-commit` merge reveals too many conflicts (e.g., 50+ files), abort and prepare by breaking the upgrade into smaller steps.

```bash
git merge --abort
git stash pop

# Option A: cherry-pick specific upstream commits in order
git cherry-pick <commit1> <commit2> ...

# Option B: wait until you can dedicate time to resolve all conflicts
```

## Related

- `docs/solutions/workflow-issues/git-staging-timing-checkout-edit.md` — A git staging pitfall encountered during this upgrade where `git checkout --` stages a snapshot rather than the working tree, causing unexpected behavior when editing files mid-merge.
- `docs/solutions/workflow-issues/upstream-api-divergence-cherry-pick-port.md` — Documents how upstream API divergence turns a simple cherry-pick into a porting exercise requiring feature adaptation, directly relevant to Step 5.
- `docs/solutions/workflow-issues/pnpm-install-after-upstream-merge.md` — The missing `pnpm install` step that causes confusing build failures after merging upstream package.json changes, addressed in Step 6.
