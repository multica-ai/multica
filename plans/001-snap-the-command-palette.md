# 001 — Snap the command palette

- **Status**: DONE
- **Commit**: 3d37828e9
- **Severity**: HIGH
- **Category**: Purpose & frequency
- **Estimated scope**: 2 files, about 10 lines including a regression test

## Problem

The global search shortcut toggles a command palette that always plays a 100ms fade-and-zoom keyframe. Command palettes are keyboard-driven, potentially 100+ times/day surfaces and should open and close immediately.

```tsx
// packages/views/layout/global-shortcuts.tsx:99 — current
if (actionId === "openSearch") {
  useSearchStore.getState().toggle();
  return;
}

// packages/views/search/search-command.tsx:699 — current
<DialogContent
  finalFocus={false}
  className="top-[20%] translate-y-0 overflow-hidden rounded-xl! p-0 sm:max-w-xl!"
  showCloseButton={false}
>
```

The shared dialog primitive supplies `data-open:animate-in`, `data-open:zoom-in-95`, and matching exit classes in `packages/ui/components/ui/dialog.tsx:53-56`.

## Target

Override animation only for SearchCommand by adding `data-open:animate-none data-closed:animate-none` to its existing `DialogContent` class. Leave normal modal motion untouched.

```tsx
className="top-[20%] translate-y-0 overflow-hidden rounded-xl! p-0 sm:max-w-xl! data-open:animate-none data-closed:animate-none"
```

Add a focused assertion in `packages/views/search/search-command.test.tsx` that the rendered dialog content contains both state-specific `animate-none` classes.

## Repo conventions to follow

- Shared primitives remain business-logic free; this surface-specific override belongs on `SearchCommand`.
- Existing `SearchCommand` tests live beside the component in `packages/views/search/search-command.test.tsx` and render through `I18nWrapper`.
- Use the existing Tailwind/Base UI `data-open` and `data-closed` state variants already used by the shared dialog.

## Steps

1. Add the two state-specific `animate-none` classes to `DialogContent` in `packages/views/search/search-command.tsx`.
2. Add one regression test to `packages/views/search/search-command.test.tsx` that opens SearchCommand and asserts the dialog content carries both overrides.

## Boundaries

- Do NOT change the global shortcut, search-store behavior, shared Dialog primitive, overlay, focus handling, layout, or search results.
- Do NOT remove animations from ordinary modals.
- Do NOT add dependencies.
- If the cited component has drifted since commit `3d37828e9`, STOP and report instead of improvising.

## Verification

- **Mechanical**: from the repo root, run `pnpm --filter @multica/views test -- search-command.test.tsx`, `pnpm --filter @multica/views typecheck`, and `pnpm --filter @multica/views lint`; all must exit 0.
- **Feel check**: trigger global search repeatedly from its keyboard shortcut and confirm the palette appears/disappears on the same frame without scale or opacity interpolation. At 10% playback, verify no popup animation appears. Open a normal dialog and confirm its existing animation remains.
- **Done when**: only SearchCommand snaps, its regression test passes, and shared dialogs are unchanged.
