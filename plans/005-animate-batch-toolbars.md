# 005 — Add restrained feedback to batch toolbars

- **Status**: DONE
- **Commit**: 002ea0d87
- **Severity**: LOW
- **Category**: Purpose & frequency
- **Estimated scope**: 4 files, roughly 90–140 lines including tests

## Problem

The issue and agent batch-action toolbars are conditional floating surfaces. Selecting the first row causes a bordered toolbar to appear at the bottom of the view in one frame; clearing the final selection removes it in one frame. This is a feedback gap, but the control can appear tens of times per day, so the fix must be nearly imperceptible and must not animate its internal data or buttons.

```tsx
// packages/views/issues/components/batch-action-toolbar.tsx:81 — current
if (count === 0) return null;

// ...handlers...

return (
  <>
    <div
      className={cn(
        "z-50 flex items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg",
        placement === "fixed-bottom"
          ? "fixed bottom-6 left-1/2 -translate-x-1/2"
          : "mb-2 w-fit",
      )}
    >
```

```tsx
// packages/views/agents/components/agent-batch-toolbar.tsx:70 — current
if (rows.length === 0) return null;

// ...handlers...

return (
  <>
    <div className="absolute bottom-6 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg">
```

Both components are kept mounted by their parents while the selection changes (`packages/views/issues/surface/issue-surface.tsx:181-185,320` and `packages/views/agents/components/agents-page.tsx:1154-1159`), so they can own an `AnimatePresence` exit without changing parent APIs.

## Target

Add one short entrance/exit to the toolbar surface while keeping the positioning wrapper static.

- Import `AnimatePresence`, `motion`, and `useReducedMotion` from `motion/react` in both toolbar files.
- Import `UI_EASE_OUT` and `UI_MOTION_DURATION` from `@multica/ui/lib/motion` as created by plan 001.
- Remove the early `return null`; conditionally render only the toolbar and its dialogs inside the normal return.
- Split each toolbar into:
  1. an outer positioning wrapper that owns `fixed`/`absolute`, `bottom-6`, `left-1/2`, `-translate-x-1/2`, `z-50`, and inline placement classes;
  2. an inner `motion.div` that owns the current flex, border, background, padding, radius, and shadow classes.
- Normal-motion targets for the inner surface:
  - initial: `{ opacity: 0, transform: "translateY(8px)" }`
  - animate: `{ opacity: 1, transform: "translateY(0)" }`
  - exit: `{ opacity: 0, transform: "translateY(8px)" }`
  - enter: `{ duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT }`
  - exit: `{ duration: UI_MOTION_DURATION.micro, ease: UI_EASE_OUT }`
- Reduced-motion targets:
  - transforms remain `"translateY(0)"`
  - opacity uses `UI_MOTION_DURATION.fast` for enter and exit.
- Use `AnimatePresence initial={false}`. This prevents a toolbar already present on initial render from playing an unrelated page-load entrance, while still animating later selection changes.
- Use complete transform strings, never Motion `y` shorthand.
- Do not animate the selected count, picker values, individual buttons, or dialog content.

## Repo conventions to follow

- `packages/views/issues/components/batch-action-toolbar.tsx:159-224` and `packages/views/agents/components/agent-batch-toolbar.tsx:126-182` share the same visual toolbar pattern; keep their motion values identical.
- `packages/views/layout/navigation-progress.tsx:32` uses a short opacity transition for frequent feedback; this plan stays similarly restrained.
- The outer wrapper is required because both existing toolbars use `-translate-x-1/2` for centering. Animating a full transform on that same element would overwrite or fight the centering transform.
- Plan 001 provides the exact easing `[0.23, 1, 0.32, 1]` and 100/150ms duration constants. Do not redefine them locally.

## Steps

1. Complete plan 001 first and verify `@multica/ui/lib/motion` resolves.
2. In `packages/views/issues/components/batch-action-toolbar.tsx`, add Motion/shared-constant imports and `const shouldReduceMotion = useReducedMotion() ?? false`.
3. Move `ids` above the old early return, remove that return, and wrap the toolbar in `AnimatePresence initial={false}` with a `count > 0` conditional.
4. Split issue-toolbar positioning from the animated visual surface exactly as described. Preserve the distinct `fixed-bottom` and `inline` placements.
5. Keep the `AlertDialog` outside the animated toolbar surface but render it only while `count > 0`. When `count` becomes zero, close `statusOpen`, `priorityOpen`, `assigneeOpen`, and `deleteOpen` so no picker portal or confirmation dialog outlives the 100ms toolbar exit. Implement this with a narrow effect keyed only by `count`; do not change selection ownership.
6. In `packages/views/agents/components/agent-batch-toolbar.tsx`, add the same imports and reduced-motion hook, remove the early return, and wrap the toolbar in `AnimatePresence initial={false}` with a `rows.length > 0` conditional.
7. Split the agent toolbar's absolute centering wrapper from its animated visual surface. Render its confirmation dialogs only while `rows.length > 0` and reset their open state if rows becomes empty.
8. Update `packages/views/issues/components/batch-action-toolbar.test.tsx` with a rerender regression covering selected → empty and asserting the picker surface eventually leaves the DOM after the presence exit. Preserve the existing initial-empty test.
9. Update `packages/views/agents/components/agent-batch-toolbar.test.tsx` with the equivalent rows → empty regression. Do not assert generated inline styles or exact timer internals.

## Boundaries

- Do NOT change selection ownership, mutation behavior, common-field derivation, action order, permissions, confirmation rules, picker wiring, or toast copy.
- Do NOT move the toolbar's final resting position or change `fixed-bottom` versus `inline` behavior.
- Do NOT animate the centering wrapper, selected count, buttons, popovers, or dialogs.
- Do NOT add scale, spring, bounce, blur, stagger, or more than 8px of travel; this is a high-frequency control.
- Do NOT use Motion `x`, `y`, or `scale` shorthand properties.
- Do NOT add dependencies.
- If either parent starts conditionally unmounting the toolbar component itself after commit `002ea0d87`, STOP and report; exit presence must live above that unmount boundary.

## Verification

- **Mechanical**:
  - `pnpm --filter @multica/views typecheck`
  - `pnpm --filter @multica/views test -- issues/components/batch-action-toolbar.test.tsx issues/components/batch-action-toolbar.confirm.test.tsx agents/components/agent-batch-toolbar.test.tsx`
  - All commands must exit 0; picker values, action ordering, permissions, and confirmation behavior must remain unchanged.
- **Feel check**: in issue list/table and the agent list, select one row, add more rows, clear one row, and clear the final row.
  - Only the zero → one and one → zero boundaries may animate. Count changes while the toolbar is visible must be immediate.
  - The toolbar must remain horizontally centered for every animation frame.
  - Open a picker, then clear selection through an available path; no orphaned popup may remain after the toolbar exits.
  - Spam selection on/off and confirm CSS/Motion retargets smoothly without a delayed queue.
  - In DevTools Animations at 10% playback, confirm only opacity and a full `translateY(...)` transform animate; the outer centering wrapper must not move.
  - Toggle `prefers-reduced-motion: reduce` and confirm the 8px movement disappears while the short opacity feedback remains.
- **Done when**: both toolbar implementations share the same restrained timing, preserve centering and all actions, animate only selection-boundary appearance, and leave no popup or dialog behind.
