---
title: "Git staging timing: checkout -- stages snapshot, not working tree"
date: 2026-07-10
category: workflow-issues
module: git
problem_type: workflow_issue
component: development_workflow
severity: medium
tags: [git, staging, checkout, cherry-pick, ci-failure, porting]
---

# Git staging timing: checkout -- stages snapshot, not working tree

## Context

When porting a feature branch onto a different base (e.g. upstream `origin/main`), a common workflow is:

```bash
git checkout new-branch origin/main
git checkout main -- path/to/file.ts   # bring file from main into working tree
# ... adapt file (size tokens, API changes) ...
git add path/to/file.ts
git commit
```

The subtle pitfall: `git checkout main -- path/to/file.ts` stages the **snapshot from main** at the moment of checkout. Any subsequent `Edit` tool changes update the **working tree only** — they are NOT automatically re-staged. If you commit without re-staging, the staged version (old, unadapted) is captured, not your adapted working-tree version.

## Guidance

After checking out files from another branch and then editing them in the working tree, always **explicitly `git add` the files again before committing** to capture the working-tree edits — the `git checkout <branch> --` operation snapshots the file content into the staging area, and subsequent edits to the working tree are NOT automatically reflected in the staging area until you `git add` again.

## Why This Matters

The staged version (from `git checkout`) and the working-tree version (after your edits) diverge silently. Committing captures the staged version. The working-tree version — which has your adaptations — is left behind as an uncommitted diff. This causes CI failures (type errors from unadapted API calls) that don't reproduce locally (where you're testing the working-tree version, not the committed version).

## When to Apply

- Porting a feature onto a different base (upstream fork, different branch).
- Cherry-picking and then adapting commits for a diverged target.
- Any workflow where `git checkout <ref> -- <file>` is followed by edits to that file.

## Examples

**Before (broken — commits the unadapted version):**

```bash
git checkout main -- packages/ui/actor-chip.tsx   # stages main's version (size=14)
# Edit tool: change size={14} → size="xs" (AvatarSize token)  ← working tree only
git add packages/ui/actor-chip.tsx   # was already staged; no-op if content unchanged in index
git commit
# CI: 'Type number is not assignable to AvatarSize' ← committed version has size=14
```

**After (correct — re-stages after editing):**

```bash
git checkout main -- packages/ui/actor-chip.tsx   # stages main's version
# Edit tool: adapt size token   ← updates working tree
git add packages/ui/actor-chip.tsx   # re-stages the adapted version
git commit
# CI passes
```

**Alternative — verify before committing:**

```bash
git diff --cached -- packages/ui/actor-chip.tsx   # inspect what's staged
# Confirm staged content matches your adaptations, not the checkout snapshot
```

## Prevention

- After any `git checkout <ref> -- <file>` followed by edits, `git add` again before committing.
- Use `git diff --cached` to verify staged content matches your intent.
- Prefer `git diff HEAD -- <file>` after committing to confirm the committed version has your changes.
- In tool-assisted workflows (AI agents editing files), the risk is higher because the agent may edit a file after the checkout staged the old version — always re-stage after a batch of edits.

## Related

- This was discovered while porting a React feature onto a diverged upstream base for an open-source PR (multica-ai/multica#5199). The committed chip had `const AVATAR_SIZE = 14` (number) instead of the adapted `const AVATAR_SIZE: AvatarSize = "xs"` (string union), causing CI typecheck failure on upstream's ActorAvatar which expects `AvatarSize`.
