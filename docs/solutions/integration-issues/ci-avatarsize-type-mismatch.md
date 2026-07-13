---
title: "CI typecheck fails — AvatarSize type mismatch in ActorMentionChip"
date: 2026-07-13
category: integration-issues
module: "packages/ui/components/common"
problem_type: integration_issue
component: actor-mention-chip
symptoms:
  - "CI frontend job fails with TS2322: Type 'number' is not assignable to type 'AvatarSize | undefined'"
  - "Works locally but fails in CI because local main has a different ActorAvatar API"
root_cause: type_divergence
resolution_type: code_fix
severity: medium
tags:
  - ci
  - typecheck
  - avatar
  - mention-chip
  - type-mismatch
related_components:
  - actor-mention-chip.tsx
  - actor-avatar.tsx
---

# CI typecheck fails — AvatarSize type mismatch in ActorMentionChip

## Problem

The PR's `ActorMentionChip` component passes `size={14}` (a raw pixel number) to `ActorAvatar`'s `size` prop. On the feature branch (where `ActorAvatar` had a `showName` prop extension), this worked because the `size` prop accepted numbers. On origin/main, `ActorAvatar.size` only accepts `AvatarSize` — a string enum (`"xs" | "sm" | "md" | "lg" | "xl" | "2xl"`).

The CI builds against origin/main's types, so it fails with:
```
components/common/actor-mention-chip.tsx(117,11): error TS2322:
Type 'number' is not assignable to type 'AvatarSize | undefined'.
```

## Symptoms

- CI `frontend` job fails on typecheck step.
- Local `pnpm typecheck` may pass because the local `ActorAvatar` has been extended with number support (from a prior customization commit on local main).
- The error is invisible locally but blocks the PR.

## What Didn't Work

1. **Using `size="sm"` (20px)** — too large for mention chips which need to fit in a 14px line-box.
2. **Keeping `size={14}`** — works on the local branch but fails on origin/main's stricter type.

## Solution

Changed the `size` prop from a raw number to the closest `AvatarSize` enum value:

```tsx
// Before (on the feature branch, where ActorAvatar accepted numbers):
<ActorAvatar ... size={AVATAR_SIZE} />  // AVATAR_SIZE = 14

// After (compatible with origin/main's AvatarSize enum):
<ActorAvatar ... size="xs" />  // "xs" = 16px, closest to 14px
```

The visual difference (16px vs 14px) is negligible for mention chips. The key is type safety across the upstream boundary.

## Why This Works

`AvatarSize` is a string enum defined in `packages/ui/lib/avatar-size.ts`:
```ts
export type AvatarSize = "xs" | "sm" | "md" | "lg" | "xl" | "2xl";
export const AVATAR_SIZE_PX: Record<AvatarSize, number> = {
  xs: 16, sm: 20, md: 24, lg: 32, xl: 40, "2xl": 56,
};
```

Using the enum ensures the component compiles against both the local customization (which may extend `ActorAvatar` to accept numbers) and the upstream base (which only accepts the enum). The `style` prop still controls the exact pixel size for the chip's internal layout.

## Prevention

- **When cherry-picking commits from a feature branch onto origin/main**, verify that type signatures haven't diverged. Local customizations may relax types that upstream enforces strictly.
- **CI typecheck is the source of truth for type compatibility** — local `pnpm typecheck` may pass against relaxed types that CI's stricter base rejects.
- **Prefer semantic types over raw values** for component props that have an established enum. `"xs"` is self-documenting; `14` is a magic number.

## Related Artifacts

- Fix commit: PR #5346 (squashed into `feat(ui): add ActorMentionChip component`)
- Related test fix: squad avatar assertion updated from `rounded-md` to `rounded-full` (origin/main renders all avatars as circles)
