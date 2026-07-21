# 003 — Transition runtime skill import phases

- **Status**: DONE
- **Commit**: 002ea0d87
- **Severity**: MEDIUM
- **Category**: Missed opportunities
- **Estimated scope**: 2 files, roughly 80–120 lines including tests

## Problem

The runtime-local skill import flow has four materially different phases: selection, import progress, conflict resolution, and summary. Both the scrollable content and the sticky footer swap synchronously when `bulkState.phase` changes. The flow is occasional and stateful, so instant replacement makes completion and conflict detection harder to parse.

```tsx
// packages/views/skills/components/runtime-local-skill-import-panel.tsx:910 — current
const middle = (() => {
  if (bulkState.phase === "importing") {
    // ...progress UI...
  }

  if (bulkState.phase === "done" || bulkState.phase === "cancelled") {
    return <BulkImportSummary results={bulkState.results} />;
  }

  if (bulkState.phase === "resolving") {
    return (
      <ConflictResolutionPanel
        conflicts={pendingConflicts}
        resolutions={conflictResolutions}
        // ...callbacks...
      />
    );
  }

  // ...idle selection UI...
})();
```

```tsx
// packages/views/skills/components/runtime-local-skill-import-panel.tsx:1204 — current
<div
  ref={scrollRef}
  style={fadeStyle}
  aria-disabled={importing || undefined}
  className={`min-h-0 flex-1 overflow-y-auto px-5 py-3 ${
    importing && bulkState.phase !== "importing" ? "pointer-events-none opacity-60" : ""
  }`}
>
  {middle}
  {bulkState.phase === "idle" && (
    <p className="mt-3 text-xs text-muted-foreground">
      {t(($) => $.runtime_import.ignored_files_hint)}
    </p>
  )}
</div>
```

The footer independently branches over the same phase at `packages/views/skills/components/runtime-local-skill-import-panel.tsx:1221-1301`, so it can change one frame apart from the content if only one area is animated.

## Target

Keep the scroll container and footer shell stable. Animate only one keyed child inside each.

- Import `AnimatePresence`, `motion`, and `useReducedMotion` from `motion/react`.
- Import `UI_EASE_OUT` and `UI_MOTION_DURATION` from `@multica/ui/lib/motion` as created by plan 001.
- Use `bulkState.phase` as the key. Progress count and result updates must not change the key, so live importing rows update in place without replaying an entrance.
- Middle-content normal-motion targets:
  - initial: `{ opacity: 0, transform: "translateY(8px)" }`
  - animate: `{ opacity: 1, transform: "translateY(0)" }`
  - exit: `{ opacity: 0, transform: "translateY(-8px)" }`
  - enter: `{ duration: UI_MOTION_DURATION.standard, ease: UI_EASE_OUT }`
  - exit: `{ duration: UI_MOTION_DURATION.micro, ease: UI_EASE_OUT }`
- Middle-content reduced-motion targets:
  - transforms remain `"translateY(0)"`
  - opacity enter and exit each use `UI_MOTION_DURATION.fast`.
- Footer targets:
  - opacity only; no transform because moving a contextual action button while it becomes interactive harms targeting.
  - enter: opacity `0 → 1`, `UI_MOTION_DURATION.fast`, `UI_EASE_OUT`
  - exit: opacity `1 → 0`, `UI_MOTION_DURATION.micro`, `UI_EASE_OUT`
- Use `<AnimatePresence mode="wait" initial={false}>` for both regions so two phase states never overlap.
- Use full `transform` strings, never Motion `y` shorthand.
- Do not animate individual result rows, progress values, count cards, or conflicts. These are data the user must read.

## Repo conventions to follow

- `packages/views/skills/components/runtime-local-skill-import-panel.tsx:1204-1219` already keeps a stable scroll container; preserve it and animate its child.
- `packages/views/skills/components/runtime-local-skill-import-panel.tsx:1221-1301` already keeps a stable footer shell; preserve its border, background, and dimensions.
- `packages/views/onboarding/steps/step-first-issue.tsx:93-125` places phase motion at the root instead of staggering inner data.
- Plan 001 provides the exact easing `[0.23, 1, 0.32, 1]` and 100/150/200ms duration constants. Do not redefine them locally.

## Steps

1. Complete plan 001 first and verify `@multica/ui/lib/motion` resolves.
2. Add Motion and shared-constant imports to `packages/views/skills/components/runtime-local-skill-import-panel.tsx`, plus `const shouldReduceMotion = useReducedMotion() ?? false` in `RuntimeLocalSkillImportPanel`.
3. Leave the `middle` IIFE and every phase's business logic intact.
4. Inside the existing scroll container, wrap `{middle}` and the idle hint in one keyed `motion.div` within `AnimatePresence mode="wait" initial={false}`. Apply the exact middle-content targets above.
5. Refactor the footer's conditional fragments into a local `footerContent` value without changing handlers, labels, disabled conditions, or ordering.
6. Inside the existing footer shell, render `footerContent` in a keyed `motion.div` with `className="flex min-w-0 flex-1 items-center gap-3"` and the exact opacity-only timing above.
7. Update `packages/views/skills/components/runtime-local-skill-import-panel.test.tsx` to keep the existing conflict-to-done regression and add an assertion that the “Done” action remains callable after the presence transition. Use `findByRole`/`waitFor`; do not assert Motion-generated inline styles.

## Boundaries

- Do NOT change import ordering, mutation sequencing, cancellation, conflict resolution, query refreshes, scroll-fade behavior, copy, result counts, or navigation callbacks.
- Do NOT stagger result rows or animate progress width beyond the existing `Progress` behavior.
- Do NOT animate the scroll container's height, the summary grid layout, or the footer shell dimensions.
- Do NOT use Motion `x`, `y`, or `scale` shorthand properties.
- Do NOT add dependencies.
- If the phase IIFE or footer branches have drifted since commit `002ea0d87`, STOP and report instead of improvising.

## Verification

- **Mechanical**:
  - `pnpm --filter @multica/views typecheck`
  - `pnpm --filter @multica/views test -- skills/components/runtime-local-skill-import-panel.test.tsx`
  - Both must exit 0; all single import, bulk import, conflict, cancellation, and completion tests must remain green.
- **Feel check**: exercise idle → importing → resolving → done, idle → importing → done, and idle → importing → cancelled.
  - Each phase must replace the prior phase in at most 300ms total.
  - The footer must change in the same beat as the middle without moving its outer border.
  - Live progress increments and incoming result rows must not replay the phase entrance.
  - Rapid cancellation or immediate conflict resolution must retarget cleanly without flashing a stale phase.
  - In DevTools Animations at 10% speed, confirm the middle uses only opacity plus a full `translateY(...)` transform and the footer uses opacity only.
  - Toggle `prefers-reduced-motion: reduce` and confirm all vertical movement disappears while phase crossfades remain.
- **Done when**: every existing import path behaves identically, phase changes are visually legible, data updates stay immediate, and the animation never blocks interaction past 300ms.
