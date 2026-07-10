---
title: "Always run pnpm install after merging upstream changes"
date: 2026-07-10
category: "workflow-issues"
module: "git/pnpm"
problem_type: "workflow_issue"
component: "development_workflow"
severity: "medium"
applies_when:
  - "Merging upstream changes that modify package.json or add new dependencies"
  - "Frontend build fails with Module not found after a git merge or rebase"
tags: [git, merge, upstream, pnpm, dependency-install, build-failure, post-merge]
---

# Always run pnpm install after merging upstream changes

## Context

After merging upstream v0.3.42 into a locally customized Multica instance (self-hosted, PM2-managed, Go backend + Next.js frontend), the frontend build failed with a missing module error:

```
Module not found: Can't resolve 'react-easy-crop' from 'packages/views/common/avatar-crop-dialog.tsx'
```

The dependency `react-easy-crop` had been added in upstream PR #5074 (`feat(views): unify avatar upload with crop editing`) and was correctly declared in `packages/views/package.json` and resolved in `pnpm-lock.yaml`. However, it was never actually installed into `node_modules`. The TypeScript typecheck and Go server build produced analogous errors (`Cannot find module 'react-easy-crop'`), because the workspace-level TypeScript resolution also depends on the physical module being present.

The root cause was simple: `git merge origin/main` applies file-level changes (package.json, pnpm-lock.yaml) but does not execute any install step. The lockfile records what *should* be installed, and package.json declares what *should* be installed, but neither action runs the install itself.

## Guidance

When a git merge or rebase introduces changes to any `package.json` or `pnpm-lock.yaml`, run `pnpm install` before attempting any build, typecheck, or test command:

```bash
# After merge completes:
pnpm install

# Then build/typecheck/test as usual:
pnpm typecheck
pnpm build
```

If the merge touches `pnpm-workspace.yaml`, `catalog:` entries, or adds/removes workspaces, `pnpm install` must run as well — pnpm's workspace protocol resolution only takes effect at install time.

For PM2-managed deployments, also run `pnpm install` on the server instance, then restart the process:

```bash
pm2 restart multica-frontend
```

Add this to your standard merge checklist:

```
1. git fetch origin
2. git merge origin/main (or rebase)
3. pnpm install          # <-- always, even if lockfile didn't look like it changed
4. pnpm typecheck        # verify before deploy
5. make dev / pm2 restart
```

## Why This Matters

The mismatch between declared dependencies and installed modules produces build failures that look like code bugs but are actually workflow gaps. Specifically:

- **`react-easy-crop` was introduced by upstream PR #5074** and added to `packages/views/package.json` and the root `pnpm-lock.yaml`. The file `packages/views/common/avatar-crop-dialog.tsx` imports it directly. Without the install step, the import resolves against nothing.

- **Lockfile-only changes are invisible.** When `pnpm-lock.yaml` is updated during a merge, `git diff` shows the change, but there is no build-time guard that catches the gap between "lockfile says X should exist" and "node_modules actually contains X."

- **CI avoids this because CI always runs `pnpm install`.** Local workflows do not, which is why this failure mode is specific to developer machines and self-hosted deployments.

The consequence of skipping this step is a cascading failure: the frontend fails to compile, TypeScript reports module-not-found errors across workspaces, and the Go server (which invokes type-checking for the frontend as part of `make check`) also fails. All from a single missing install.

## When to Apply

- After any `git merge` or `git rebase` that modifies `package.json` in any workspace
- After any merge that updates `pnpm-lock.yaml` or `pnpm-workspace.yaml`
- After switching branches where the target branch has different dependency declarations than the source
- Before `make dev`, `pnpm dev:web`, `pnpm build`, `pnpm typecheck`, or `pm2 restart` following a merge

## Examples

**Before (the failing scenario):**

```bash
git fetch origin
git merge origin/main
# Merge succeeded — package.json now has react-easy-crop, but node_modules doesn't
pnpm build
# ERROR: Module not found: Can't resolve 'react-easy-crop'
```

**After (the correct workflow):**

```bash
git fetch origin
git merge origin/main
pnpm install        # installs react-easy-crop and any other new deps
pnpm typecheck      # passes — react-easy-crop resolves
pnpm build          # passes
make dev            # starts cleanly
```

**On the self-hosted server:**

```bash
cd /path/to/multica
git fetch origin
git merge origin/main
pnpm install
pm2 restart multica-frontend
```

## Related

- Upstream PR #5074 (`feat(views): unify avatar upload with crop editing`) introduced the `react-easy-crop` dependency
- Merge commit `2d34a8d0b` (local merge commit on `main`; SHA may rewrite on rebase) exposed this gap
- Multica developer conventions: https://multica.ai/docs/developers/conventions
