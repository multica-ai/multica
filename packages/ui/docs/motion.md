# Motion

Import durations and curves from `@multica/ui/lib/motion`.

## Usage

Motion explains a change where it happens: where the selection went, what opened, what was removed, why the content below moved. Every interaction that changes layout in response to the user or to live data gets motion by default; a feature is not done when it only works.

| Token | Value | Use |
| --- | --- | --- |
| `UI_MOTION_DURATION.micro` | 100ms | Exits, icon swaps, hover and press feedback |
| `UI_MOTION_DURATION.fast` | 150ms | Entries, row add/remove, small size changes |
| `UI_MOTION_DURATION.standard` | 200ms | Indicators, expand/collapse, container size changes |
| `UI_EASE_OUT` / `UI_EASE_OUT_CSS` | `cubic-bezier(0.23, 1, 0.32, 1)` | Anything entering, moving or resizing |
| `UI_EASE_IN` | `cubic-bezier(0.4, 0, 1, 1)` | Anything leaving |

- Keep UI motion at 200ms or less. An exit is shorter than its entry.
- Never block input. A click during an animation acts on the end state, and the animation retargets from where it is.
- In class names, write the curve as `ease-[cubic-bezier(0.23,1,0.32,1)]`; in JavaScript, use the constants.

## Rules by interaction

### Overlays

Dialog, AlertDialog, Sheet, Popover, DropdownMenu, ContextMenu, Select, HoverCard, Tooltip.

- Enter: fade and scale from 95% toward the trigger (Sheet slides in from its edge), 100–150ms, `UI_EASE_OUT`. Exit: reverse, at most as long.
- Use the components in `packages/ui/components/ui/` as they are. Dialog timing lives in the `--dialog-*` tokens (`dialog.md`); do not override an overlay's animation at a call site.
- Keep the content mounted until the exit finishes. Do not gate the popup's children with `{open && …}`, and do not unmount the root when it closes. Base UI's Portal unmounts after the exit. A host that renders one modal at a time keeps the last modal and its data until `onOpenChangeComplete(false)`.
- Tooltips that open instantly while the pointer moves across a toolbar skip their animation (`data-[instant]:animate-none`).

### Expand and collapse

Sections, tree nodes, disclosures, "Show N more", inline hints.

- Animate height and opacity together, 150–200ms, `UI_EASE_OUT`. The content below moves with the change; it never jumps.
- Rotate one `ChevronRight` by 90° on the same duration. Do not swap two icons.
- Use `CollapsibleContent`. For a conditional block outside a Collapsible, use a `grid-rows-[0fr]` → `grid-rows-[1fr]` transition, as `CreateRunHint` in `packages/views/modals/create-issue.tsx` does, or motion's `height: "auto"`.

### List rows added and removed

Archive, complete, snooze, a new notification, a new run.

- A removed row collapses its height, margins and opacity, 150ms, `UI_EASE_OUT`, inside `AnimatePresence initial={false}`. Rows below close the gap; they do not jump.
- A row the user just added, or one that arrived live, fades in, with at most 4px of upward movement, 150ms.
- Rows on first render and rows a virtual list mounts while scrolling never animate.

### Content switch

Tabs, views, a filter that changes a list, the next or previous item in a detail pane.

- The old content fades out (100ms, `UI_EASE_IN`), then the new content fades in (150ms, `UI_EASE_OUT`).
- While the next content loads, the container keeps its height, then eases to the new height (200ms). It never collapses to 0 and grows back.
- Slide only when the order means something, such as next and previous: at most 12px, 160ms (`.animate-inbox-enter-next` / `-prev`).
- A loading placeholder uses `.animate-skeleton`, which stays hidden for its first 200ms, so fast data never flashes a skeleton.

### Selected-state indicator

Tabs and segmented controls.

- The indicator moves from the old option to the new one; the labels stay put. Use the indicator the shared control renders (`Tabs`, `SegmentedToggle`), with the transition it ships. Do not build a per-call-site indicator.
- Controls whose labels change width during the switch, such as tabs that show only an icon while closed, or a header that gains or loses a button, cannot use a layout animation: it measures a box that is still moving and overshoots. Measure the start and end of every part once before paint, animate them all on one curve, and tween the indicator on that same curve and duration from where it is straight to the new tab's measured end box. Do not derive it from the boxes while they move: blending moving boxes bulges past the end when the new tab starts far from where it settles. `useTabSwitchMotion` in `packages/views/inbox/components/inbox-tabs.tsx` is the reference.

### State and value feedback

Counts, progress, copy, send/stop, save state, toggles.

- Numbers that change in place use `NumberFlow`.
- Progress bars and rings transition the property that changes (`transition-[width]`, `transition-[stroke-dashoffset]`), 150–200ms.
- An icon that swaps to confirm an action (copy → check, send → stop) crossfades with a small scale, about 140ms: `CopyCheckIcon` for copy.
- Color and background state changes use `transition-colors`.

## Reduced motion

The Appearance setting "Reduce motion" and the system's `prefers-reduced-motion` both mean instant: no movement, no size animation, no fade in its place.

- The Appearance setting zeroes CSS animation and transition durations for the whole document. The system setting reaches CSS only through media queries, so every CSS animation or transition also carries `motion-reduce:animate-none` or `motion-reduce:transition-none`.
- JavaScript motion (motion/react, WAAPI) reads `useReducedMotion()` from `@multica/core/appearance`, or `prefersReducedMotion()` outside render, which cover both settings. With either set, pass `duration: 0` or skip the animation. motion's own `useReducedMotion` and a bare `matchMedia` check see only the system setting. `MotionProvider` at the app root makes motion/react skip transform and layout animation under either setting, but opacity and color still animate, so the `duration: 0` above still applies.
- Test the reduced branch: with reduced motion, the end state is there in the same frame.

## Do not animate

These stay instant:

- Switching the body of a Desktop tab (`tab-content.tsx`).
- J/K and other rapid keyboard navigation, such as stepping through issues in `IssuePeek`.
- Rows a virtualized list mounts while scrolling or restoring history. Only content that arrives live, or that the user just triggered, may animate.
- Anything that follows the pointer: resizing panels by drag (`data-sidebar-resizing`, `data-resizing`), and panning or zooming a canvas.

## Performance

- In Virtuoso and other virtualized lists, animate only rows that arrived live or that the user just acted on, once, with WAAPI on the row (`useNewRunIds`, `useRunCommentMotion`). Do not wrap long virtualized content in Base UI's `Collapsible.Panel`.
- Do not read layout on every frame of an animation: no `getBoundingClientRect()` in `onUpdate` or a `requestAnimationFrame` loop. Measure once in a layout effect, then animate between the measured values.
- Prefer `transform` and `opacity`. Animate width or height only on the container whose size actually changes.
- Pause looping motion while it is off screen (`useRunAnimationVisibility`).

## Shared primitives

| Need | Use | Where |
| --- | --- | --- |
| Durations and curves | `UI_MOTION_DURATION`, `UI_EASE_OUT`, `UI_EASE_IN`, `UI_INDICATOR_SPRING` (selected-state indicators) | `packages/ui/lib/motion.ts` |
| Overlay enter and exit | The overlay components; Dialog's `--dialog-*` tokens | `packages/ui/components/ui/`, `packages/ui/styles/tokens.css` |
| Expand and collapse | `CollapsibleContent` | `packages/ui/components/ui/collapsible.tsx` |
| Tabs indicator | `Tabs` | `packages/ui/components/ui/tabs.tsx` |
| Segmented control indicator | `SegmentedToggle` | `packages/views/common/segmented-toggle.tsx` |
| Reduced motion for motion/react | `MotionProvider` (mounted at the web and desktop roots) | `packages/views/common/motion-provider.tsx` |
| Tabs whose labels fold and unfold | `useTabSwitchMotion` | `packages/views/inbox/components/inbox-tabs.tsx` |
| Content switch that keeps its height while loading | `InboxTabBody` (pattern; extract it when a second caller needs it) | `packages/views/layout/panels/inbox-section.tsx` |
| Row exit collapse | Inbox row `motion.li` exit | `packages/views/layout/panels/inbox-section.tsx` |
| Right sidebar open and close | `AnimatedRightSidebar` | `packages/views/layout/animated-right-sidebar.tsx` |
| Next and previous detail | `.animate-inbox-enter-next`, `-prev`, `-fade` | `packages/ui/styles/base.css` |
| Live arrivals in virtualized lists | `useNewRunIds`, `useRunCommentMotion`, `useRunDisclosureMotion` | `packages/views/issues/components/use-run-comment-motion.ts` |
| Changing numbers | `NumberFlow` | `packages/ui/components/ui/number-flow.tsx` |
| Copy confirmation | `CopyCheckIcon` | `packages/ui/components/common/copy-check-icon.tsx` |
| Loading placeholder | `.animate-skeleton` | `packages/ui/styles/base.css` |

Add a row when you add a primitive, and point the rules above at it.

## Verification

jsdom has no layout, so unit tests cover wiring and the reduced-motion branch, not the animation. A static screenshot cannot show a jump or an overshoot either.

A UI PR whose interaction changes size, position, or what is on screen attaches a GIF of it and the results of a frame-by-frame sample from the real app. `scripts/motion/capture.mjs` does both against a `make up` environment; `scripts/motion/inbox-tabs.mjs` is a complete scenario to copy:

```bash
make up C=api,web ARGS="--ephemeral"
make env-exec ARGS="-- node scripts/dev-fixtures.mjs sidebar"
make env-exec ARGS="-- node scripts/motion/capture.mjs scripts/motion/inbox-tabs.mjs --out ../motion"
```

A scenario samples the boxes of the elements involved on every animation frame and checks, by default:

- an indicator ends exactly on its target and never overshoots on the way (`landsOn`, `monotonic`);
- a container's height changes in one direction, over several frames (`monotonic`, `animated`);
- the block below never jumps away and back (`monotonic`);
- with reduced motion, every change lands in a single frame (`instant`).

Hold the first request of a tab or page with `delay()` to reproduce what happens while it loads. The script's header lists its options and its limits.
