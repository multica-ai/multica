# 001 — Animate the attachment preview entrance and exit

- **Status**: DONE
- **Commit**: 002ea0d87
- **Severity**: MEDIUM
- **Category**: Missed opportunities
- **Estimated scope**: 4 files, roughly 80–120 lines including tests

## Problem

`packages/views/editor/attachment-preview-modal.tsx` implements a custom full-screen portal instead of using the shared dialog primitive. The modal returns `null` as soon as `open` becomes false, so the large black backdrop and preview surface appear and disappear in one frame. Because the surface can occupy almost the entire viewport, the teleport is much more visible than an instant inline state change.

```tsx
// packages/views/editor/attachment-preview-modal.tsx:247 — current
if (!open || typeof document === "undefined") return null;

return createPortal(
  <div
    className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
    onClick={onClose}
    role="dialog"
    aria-modal="true"
    aria-label={state.filename}
  >
    <div
      className="flex h-[min(90vh,calc(100vh-2rem))] w-full max-w-6xl flex-col overflow-hidden rounded-lg bg-background shadow-xl"
      onClick={(e) => e.stopPropagation()}
    >
```

There is no shared TypeScript motion vocabulary in `@multica/ui`; existing components hand-type durations and easing arrays. Implementing five related improvements that way would immediately create duplicated near-identical values.

## Target

Create one shared motion-constant module and use it to animate this portal with `AnimatePresence`.

```ts
// packages/ui/lib/motion.ts — target
export const UI_EASE_OUT = [0.23, 1, 0.32, 1] as const;

export const UI_MOTION_DURATION = {
  micro: 0.1,
  fast: 0.15,
  standard: 0.2,
} as const;
```

Export it as `@multica/ui/lib/motion` from `packages/ui/package.json`.

In `AttachmentPreviewModal`:

- Import `AnimatePresence`, `motion`, and `useReducedMotion` from `motion/react`.
- Do not return `null` merely because `open` is false. Only return `null` when `document` is unavailable; keep the presence boundary mounted so it can run the exit.
- Keep `createPortal` and the current focus, click, Escape, download, and open-in-new-tab behavior unchanged.
- Render the backdrop only while `open` is true inside `<AnimatePresence>`.
- Backdrop target:
  - initial: `{ opacity: 0 }`
  - animate: `{ opacity: 1 }`
  - exit: `{ opacity: 0 }`
  - transition: `{ duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT }`
- Preview surface target for normal motion:
  - initial: `{ opacity: 0, transform: "scale(0.95)" }`
  - animate: `{ opacity: 1, transform: "scale(1)" }`
  - exit: `{ opacity: 0, transform: "scale(0.95)" }`
  - enter transition: `{ duration: UI_MOTION_DURATION.standard, ease: UI_EASE_OUT }`
  - exit transition: `{ duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT }`
- Preview surface target when `useReducedMotion()` returns true:
  - initial/exit transform: `"scale(1)"`
  - keep the same opacity transition; reduced motion removes movement, not comprehension.
- Use the complete `transform` string. Do not use Motion's `scale` shorthand.
- The exit must mirror the entrance; closing by backdrop click, Escape, close button, or open-in-new-tab must all take the same path.

The full-screen backdrop and centered modal are exempt from trigger-origin animation. Keep the centered transform origin.

## Repo conventions to follow

- `packages/views/chat/components/chat-window.tsx:707-723` is the closest existing exemplar for a surface that combines opacity and a subtle `0.95 → 1` scale.
- `apps/desktop/src/renderer/src/components/tab-bar.tsx:9,524` demonstrates `useReducedMotion()` in the current stack.
- `packages/ui/package.json:10-25` defines explicit subpath exports for shared UI utilities; add `"./lib/motion": "./lib/motion.ts"` beside the other `./lib/*` exports.
- `packages/views/package.json:93` already declares `motion`; do not add or change dependencies.
- This plan deliberately replaces Motion shorthand with a full `transform` string to keep the animation compositor-friendly.

## Steps

1. Add `packages/ui/lib/motion.ts` with exactly `UI_EASE_OUT` and `UI_MOTION_DURATION` as shown above. Do not add unused spring or drawer constants.
2. Add the `@multica/ui/lib/motion` subpath export to `packages/ui/package.json`.
3. In `packages/views/editor/attachment-preview-modal.tsx`, add the Motion imports, shared constants, and `const shouldReduceMotion = useReducedMotion() ?? false` near the other hooks.
4. Change the server guard to `if (typeof document === "undefined") return null;` and place the `open` conditional inside `AnimatePresence` in the portal.
5. Convert the outer backdrop to `motion.div` with the exact opacity transition above.
6. Convert only the inner preview surface to `motion.div` with the exact full-transform targets above. Preserve all class names and event handlers.
7. Update `packages/views/editor/attachment-preview-modal.test.tsx` with a close-path regression test that rerenders from `open` to false and waits for the dialog to leave after the exit. Do not assert intermediate inline styles produced by Motion; assert that the dialog remains logically usable while open and is removed after exit completion.

## Boundaries

- Do NOT replace this portal with the shared `Dialog`; the preview has intentionally different viewport sizing and content dispatch.
- Do NOT change media loading, download URL resolution, keyboard handling, focus behavior, content-type dispatch, or navigation.
- Do NOT animate width, height, top, left, padding, margin, filter, or shadow.
- Do NOT use Motion `x`, `y`, or `scale` shorthand properties.
- Do NOT add dependencies.
- If the cited portal structure has drifted since commit `002ea0d87`, STOP and report instead of improvising.

## Verification

- **Mechanical**:
  - `pnpm --filter @multica/ui typecheck`
  - `pnpm --filter @multica/views typecheck`
  - `pnpm --filter @multica/views test -- editor/attachment-preview-modal.test.tsx`
  - All commands must exit 0, and the existing dispatch, download, error-state, and open-in-new-tab tests must remain green.
- **Feel check**: run web or desktop, open image, PDF, video, and text attachments, then close by backdrop, Escape, close button, and open-in-new-tab.
  - The backdrop must fade without flashing the page beneath it.
  - The preview must grow only from `0.95`, never from `0`.
  - Closing must follow the same visual path as opening.
  - In DevTools Animations, set playback to 10% and confirm that backdrop and surface remain synchronized and neither width nor height animates.
  - Toggle `prefers-reduced-motion: reduce` and confirm the scale disappears while the 150ms opacity transition remains.
- **Done when**: every open and close path retains its behavior, the surface uses only opacity plus a full transform string, and reduced-motion users see a fade without scale.
