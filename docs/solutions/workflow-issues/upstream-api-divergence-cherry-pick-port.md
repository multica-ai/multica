---
title: "Upstream API divergence makes cherry-pick a port: adapt before assuming it applies"
date: 2026-07-10
category: workflow-issues
module: contributing-upstream
problem_type: workflow_issue
component: development_workflow
severity: medium
tags: [upstream, porting, cherry-pick, breaking-change, actor-avatar, type-token]
---

# Upstream API divergence makes cherry-pick a port: adapt before assuming it applies

## Context

When contributing a feature to an upstream repo after developing it on a fork with customizations, the natural assumption is that cherry-picking the feature commits onto `origin/main` will produce a clean PR. This breaks when upstream has made **breaking API changes** to the same files the feature depends on — the cherry-pick may apply textually but fail typecheck, or the feature's design assumptions may no longer hold.

## Guidance

Before cherry-picking feature commits onto upstream, **diff the upstream changes to shared dependency files** between your merge-base and `origin/main`:

```bash
mb=$(git merge-base HEAD origin/main)
git log --oneline $mb..origin/main -- packages/ui/components/common/actor-avatar.tsx
git diff $mb origin/main -- packages/ui/components/common/actor-avatar.tsx | head -50
```

If upstream changed the API surface the feature depends on (prop types, shape constraints, rendering behavior), the cherry-pick is a **port**, not a mechanical replay. Adapt the feature to the upstream API before opening the PR.

## Why This Matters

In this case, upstream made two breaking changes to `ActorAvatar` (commit `f4de0948a`):

1. **`size` prop changed from `number` to `AvatarSize` union** (`"xs" | "sm" | "md" | "lg" | "xl" | "2xl"`) with a pixel mapping via `AVATAR_SIZE_PX`. The feature's `size={14}` (number) wouldn't compile on the upstream base.

2. **All avatars unified to circles** — the `isSquad ? "rounded-md" : "rounded-full"` shape logic was removed; "every actor renders as a circle" is now a hard invariant. The feature's R2 requirement ("squad = rounded-square") was outdated.

Cherry-picking onto `origin/main` without adapting would produce a PR that fails CI typecheck (Type 'number' is not assignable to 'AvatarSize | undefined') and contradicts upstream's intentional design choice.

## When to Apply

- Contributing a feature to an upstream repo when you've been developing on a fork for a while.
- The feature touches shared dependencies (UI component APIs, type definitions, config schemas).
- Upstream has made breaking changes since your merge-base (check `git log <merge-base>..origin/main -- <dep-files>`).

## Examples

**Before (cherry-pick assumed clean, failed CI):**

```bash
git checkout -b feat/pr origin/main
git cherry-pick commit1 commit2 ...   # textual merge, semantic break
git push fork feat/pr
gh pr create   # CI: TS2322: Type 'number' is not assignable to 'AvatarSize | undefined'
```

**After (detected divergence, adapted before PR):**

```bash
# Step 1: detect divergence
git log $(git merge-base HEAD origin/main)..origin/main -- packages/ui/components/common/actor-avatar.tsx
# → f4de0948a refactor(ui): unify ActorAvatar size tiers + round all avatars

# Step 2: examine the API change
git show origin/main:packages/ui/lib/avatar-size.ts
# → AvatarSize = "xs" | "sm" | ... ; AVATAR_SIZE_PX = { xs: 16, ... }

# Step 3: apply feature state + adapt
git checkout -b feat/pr origin/main
git checkout main -- <feature files>
# Edit: size={14} → size="xs" ; drop squad rounded-md (all circles now) ; update R2
# Verify: pnpm typecheck + pnpm test

# Step 4: commit, push, PR
git add <files> && git commit
git push fork feat/pr && gh pr create
```

## Prevention

- Before starting work on a long-lived feature branch, note the merge-base SHA and the key dependency files you touch.
- When ready to contribute upstream, run the divergence check as the first step — before cherry-picking or applying any commits.
- If the divergence is substantial (breaking API change to a core dependency), expect the cherry-pick to be a port. Budget time for adapting the feature, updating tests, and re-verifying.
- Prefer applying the **net feature state** (final files) to the upstream base rather than replaying intermediate commit history — intermediate commits may touch-then-delete files, creating unnecessary conflicts.
- Document the adaptation in the PR description so reviewers understand why the upstream code looks different from your development branch.

## Related

- multica-ai/multica#5199 — the PR created by porting the avatar-chip feature onto upstream's refactored ActorAvatar.
- `packages/ui/lib/avatar-size.ts` — the upstream size token definition (`AvatarSize`, `AVATAR_SIZE_PX`).
- Commit `f4de0948a refactor(ui): unify ActorAvatar size tiers + round all avatars` — the upstream breaking change that forced the port.
