# 002 — Transition remote runtime setup into success

- **Status**: DONE
- **Commit**: 002ea0d87
- **Severity**: MEDIUM
- **Category**: Missed opportunities
- **Estimated scope**: 2 files, roughly 60–90 lines including tests

## Problem

The remote-runtime dialog passively waits for a WebSocket registration event. When it arrives, React replaces the entire instruction subtree with the success subtree in one render. This is a rare, meaningful state change, but today the interface provides no visual bridge between “waiting for a daemon” and “connected.”

```tsx
// packages/views/runtimes/components/connect-remote-dialog.tsx:100 — current
return (
  <Dialog open onOpenChange={(v) => !v && onClose()}>
    <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-lg">
      {step === "instructions" && <InstructionsStep onClose={onClose} />}
      {step === "success" && (
        <SuccessStep
          onGoToAgents={handleGoToAgents}
          onGoToRuntime={
            newRuntimeIdRef.current ? handleGoToRuntime : undefined
          }
        />
      )}
    </DialogContent>
  </Dialog>
);
```

The success content itself is static:

```tsx
// packages/views/runtimes/components/connect-remote-dialog.tsx:351 — current
<div className="flex flex-col items-center gap-3 px-6 py-8">
  <div
    className="flex h-12 w-12 items-center justify-center rounded-full bg-success/10"
    aria-hidden
  >
    <Check className="h-6 w-6 text-success" />
  </div>
</div>
```

## Target

Animate the step replacement, not the dialog shell. The shared `DialogContent` already owns the modal entrance and exit.

- Import `AnimatePresence`, `motion`, and `useReducedMotion` from `motion/react`.
- Import `UI_EASE_OUT` and `UI_MOTION_DURATION` from `@multica/ui/lib/motion` as created by plan 001.
- Keep the current `step` values and WebSocket logic unchanged.
- Inside `DialogContent`, render one keyed `motion.div` for the active step within `<AnimatePresence mode="wait" initial={false}>`.
- The wrapper must use `className="flex min-h-0 flex-1 flex-col"` so both existing fragment-based step components preserve the dialog's flex layout and scrolling.
- Normal-motion variants:
  - enter initial: `{ opacity: 0, transform: "translateY(8px)" }`
  - settled: `{ opacity: 1, transform: "translateY(0)" }`
  - exit: `{ opacity: 0, transform: "translateY(-8px)" }`
  - enter transition: `{ duration: UI_MOTION_DURATION.standard, ease: UI_EASE_OUT }`
  - exit transition: `{ duration: UI_MOTION_DURATION.micro, ease: UI_EASE_OUT }`
- Reduced-motion variants:
  - all transforms are `"translateY(0)"`
  - opacity still transitions; use `UI_MOTION_DURATION.fast` for both enter and exit.
- Use complete `transform` strings, never Motion `y` shorthand.
- Do not separately bounce or draw the checkmark. The state transition is enough; a crisp dashboard does not need a celebration layered on top.

## Repo conventions to follow

- `packages/ui/components/ui/dialog.tsx:42-80` owns the dialog shell animation. Do not duplicate or override it.
- `packages/views/onboarding/steps/step-first-issue.tsx:93-125` demonstrates that asynchronous phase content is wrapped at the phase root rather than animating each child.
- `apps/desktop/src/renderer/src/components/tab-bar.tsx:73,325-330` is the repository's current 8px/180ms entrance reference; use the full `transform` string and the shared 200ms strong ease-out instead of its Motion shorthand.
- Plan 001 provides the exact shared easing `[0.23, 1, 0.32, 1]` and durations. Do not redefine them locally.

## Steps

1. Complete plan 001 first and verify `@multica/ui/lib/motion` resolves.
2. Add Motion and shared-constant imports to `packages/views/runtimes/components/connect-remote-dialog.tsx`.
3. Add `const shouldReduceMotion = useReducedMotion() ?? false` inside `ConnectRemoteDialog`.
4. Replace the two sibling step conditionals with one keyed `motion.div` inside `AnimatePresence mode="wait" initial={false}`. Render `InstructionsStep` or `SuccessStep` inside it without editing those components' business logic or copy.
5. Put the exact enter and exit transitions on the corresponding variant targets so exit takes 100ms and the next step enters over 200ms.
6. Extend `packages/views/runtimes/components/connect-remote-dialog.test.tsx` so the mocked `useWSEvent` callback can be captured and invoked. Assert that the success title and CTA eventually replace the instructions after a `daemon:register` payload. Keep the test focused on lifecycle and content, not generated inline animation styles.

## Boundaries

- Do NOT change the WebSocket event name, query invalidation, runtime ID capture, routing, copy commands, or troubleshooting disclosure.
- Do NOT animate `DialogContent` itself; that would double-animate the modal.
- Do NOT animate height. The keyed child may change intrinsic height immediately after its exit; no layout animation is allowed.
- Do NOT add success confetti, bounce, stagger, or a checkmark keyframe.
- Do NOT use Motion `x`, `y`, or `scale` shorthand properties.
- Do NOT add dependencies.
- If the step conditionals no longer match the excerpt at commit `002ea0d87`, STOP and report instead of improvising.

## Verification

- **Mechanical**:
  - `pnpm --filter @multica/views typecheck`
  - `pnpm --filter @multica/views test -- runtimes/components/connect-remote-dialog.test.tsx`
  - Both must exit 0; existing cloud/self-host command and ligature tests must remain green.
- **Feel check**: open the remote runtime dialog, then register a daemon while the dialog is visible.
  - Instructions must leave quickly before success enters; the two states must never overlap.
  - The success CTA must be clickable as soon as the 200ms entrance completes.
  - Trigger registration immediately after opening and confirm the presence transition remains interruptible and does not replay the dialog shell entrance.
  - In DevTools Animations, use 10% playback and confirm only opacity and `transform: translateY(...)` animate.
  - Toggle `prefers-reduced-motion: reduce` and confirm position stays fixed while the content still crossfades.
- **Done when**: the WebSocket-driven state change is legible, lasts no more than 300ms total, never double-exposes both steps, and preserves all routing and setup behavior.
