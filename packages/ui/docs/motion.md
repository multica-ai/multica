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
- In Tailwind v4, `translate-*`, `scale-*` and `rotate-*` write the standalone `translate`, `scale` and `rotate` properties, not `transform`. A hand-written transition names them, as in `transition-[translate,opacity]`; with `transition-[transform,opacity]` they snap. `transition` and `transition-transform` already cover all four. The 3D `rotate-x/y/z-*` still compose `transform`. `pnpm check:ui-transitions` reports the mix-up.

## Rules by interaction

### Overlays

Dialog, AlertDialog, Sheet, Popover, DropdownMenu, ContextMenu, Select, HoverCard, Tooltip.

- Enter: fade and scale from 95% toward the trigger (Sheet slides in from its edge), 100–150ms, `UI_EASE_OUT`. Exit: reverse, at most as long.
- Use the components in `packages/ui/components/ui/` as they are. Dialog timing lives in the `--dialog-*` tokens (`dialog.md`); do not override an overlay's animation at a call site.
- Keep the content mounted until the exit finishes. Do not gate the popup's children with `{open && …}`, and do not unmount the root when it closes. Base UI's Portal unmounts after the exit. A host that renders one modal at a time keeps the last modal and its data until `onOpenChangeComplete(false)`.
- Keep the content and its data unchanged during the exit, too. `open` turns false on the exit's first frame: do not pass it to a data hook, a query's `enabled`, or an effect that resets state, or the list empties and the panel shrinks while it fades. Use `open` only to stop input, such as key listeners. The palette in `packages/views/search/search-command.tsx` calls `useRootModel(…, { active: true })` and lets the Portal unmount it after the exit.
- Tooltips that open instantly while the pointer moves across a toolbar skip their animation by zeroing its duration (`data-[instant]:animation-duration-0`). Never remove an exit animation while it plays (`animate-none` on a state that can turn on mid-exit): Base UI then never unmounts the popup, and it stays on screen.

### Expand and collapse

Sections, tree nodes, disclosures, "Show N more", inline hints.

- Animate height and opacity together, 150–200ms, `UI_EASE_OUT`. The content below moves with the change; it never jumps.
- Rotate one `ChevronRight` by 90° on the same duration. Do not swap two icons.
- Inside a Base UI `Collapsible`, use `CollapsibleContent`: the issue sidebar sections, chat tool rows, sections in a dialog. `Collapsible` wires the trigger's `aria-expanded` and `aria-controls` to it.
- Where an `open` prop drives the section and there is no `Collapsible`, use `PanelDisclosure`, as the left panels and object pages do: `PanelSection`, the tree rows in `work-tree.tsx` and `views-section.tsx`, the groups in `object-page.tsx`. It makes the exiting content `inert`, and under reduced motion unmounts it in the same frame.
- Both unmount the content once it is closed, so a collapsed list releases its queries; do not pass `keepMounted` or `hiddenUntilFound` to a panel whose content subscribes to data. Both clip only while the height moves: a resting section must not cut off its rows' focus rings.
- For a conditional block with nothing focusable in it, such as an inline hint, use a `grid-rows-[0fr]` → `grid-rows-[1fr]` transition, as `CreateRunHint` in `packages/views/modals/create-issue.tsx` does. Its inner `overflow-hidden` clips at rest too.

### List rows added and removed

Archive, complete, snooze, a new notification, a new run.

- A removed row collapses its height, margins and opacity, 150ms, `UI_EASE_OUT`, inside `AnimatePresence initial={false}`. Rows below close the gap; they do not jump.
- A row the user just added, or one that arrived live, fades in, with at most 4px of upward movement, 150ms.
- Rows on first render and rows a virtual list mounts while scrolling never animate.

### Drag and drop

Moving issues on the board, in the list, and between swimlanes.

- While dragging, the overlay is lifted with `ISSUE_DRAG_OVERLAY_CLASS`: tilted 1°, scaled to 101%, with a shadow. It follows the pointer with no easing.
- On drop, `createIssueDropAnimation` glides the overlay into its new slot and straightens it, 170ms, `UI_EASE_OUT`. The slot stays hidden with `visibility` until the overlay lands; hiding it with `opacity` would set off the slot's own opacity transition and flash it.
- A cancelled drop, an intercepted one (a workflow block or handoff, an archived status, a no-op in a sort-locked view), and any drop under reduced motion skip the glide. Clear the ref you pass to `createIssueDropAnimation` in `onDragStart`, and set it in `onDragEnd` or `onDragCancel` when the move does not land.

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

## Keep interaction intact

An animated container must not break the controls inside it, while it moves or after it settles.

- Clip only while the size changes. A permanent `overflow-hidden` cuts off the focus rings and drop rings of the rows inside it. With motion, put `overflow: "hidden"` in `initial` and `exit`, and end `animate` with `transitionEnd: { overflow: "visible" }`, as `PanelDisclosure` in `packages/views/layout/panel-nav.tsx` and `ChatBarChip` in `packages/views/chat/components/chat-bar.tsx` do. `CollapsibleContent` already clips only while its height moves. A container that clips by design, such as a drawer that slides in from an edge or a card with rounded corners, keeps its rows inside their own scroll area, with room for the ring.
- Content that is leaving takes no clicks and no focus. Read `useIsPresent()` in the component that `AnimatePresence` renders, and set `inert={!isPresent}` on the leaving element. `pointer-events-none` alone still lets Tab land on it. `CollapsibleContent` is inert while it closes.
- motion owns every size it animates. motion writes `width` and `height` as inline styles, which win over a Tailwind class for the same property, such as `max-md:w-full`. When the size depends on the breakpoint, choose motion's target by breakpoint (`useIsMobile()`), as `StepInspectorDrawer` in `packages/views/common/task-transcript/agent-transcript-dialog.tsx` does.
- A focus ring has to be drawn before it can be kept whole. In Tailwind v4, `outline-none` also sets `--tw-outline-style: none`, which `focus-visible:outline-2` reads, so on the same element the two draw nothing. Add `focus-visible:outline-solid`, as `PANEL_ROW_CLASS` in `packages/views/layout/panel-nav.tsx` does. A ring on `::after` (`focus-visible:after:outline-2`) is unaffected, because the variable does not inherit. `pnpm check:ui-outlines` reports a width left without its style.

## Reduced motion

The Appearance setting "Reduce motion" and the system's `prefers-reduced-motion` both mean instant: no movement, no size animation, no fade in its place.

- The Appearance setting zeroes CSS animation and transition durations for the whole document. The system setting reaches CSS only through media queries, so every CSS animation or transition also carries `motion-reduce:animate-none` or `motion-reduce:transition-none`.
- JavaScript motion (motion/react, WAAPI) reads `useReducedMotion()` from `@multica/core/appearance`, or `prefersReducedMotion()` outside render, which cover both settings. With either set, pass `duration: 0` or skip the animation. motion's own `useReducedMotion` and a bare `matchMedia` check see only the system setting. `MotionProvider` at the app root makes motion/react skip transform and layout animation under either setting, but opacity and color still animate, so the `duration: 0` above still applies.
- Test the reduced branch: with reduced motion, the end state is there in the same frame.
- Two checks enforce this. The `NO_SYSTEM_ONLY_REDUCED_MOTION` lint rule rejects a `prefers-reduced-motion` query string in `packages/views`, `apps/web` and `apps/desktop`. `packages/ui/styles/reduced-motion.test.ts` reads every stylesheet in `packages/ui`, `packages/views`, `apps/desktop` and `apps/web` (except the marketing site), and fails when a class starts an animation and no `@media (prefers-reduced-motion: reduce)` rule in the same file stops it; the few exceptions are listed in that test by file and class, each with its reason.

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
| Expand and collapse inside a `Collapsible` | `CollapsibleContent` | `packages/ui/components/ui/collapsible.tsx` |
| Expand and collapse driven by an `open` prop (panel sections, tree rows, object page groups) | `PanelDisclosure` | `packages/views/layout/panel-nav.tsx` |
| Issue drag lift and drop settle (board, list, swimlanes) | `ISSUE_DRAG_OVERLAY_CLASS`, `createIssueDropAnimation` | `packages/views/issues/components/drag-drop-animation.ts` |
| motion container that clips only while it resizes, and leaves without taking input | `PanelDisclosure`, `ChatBarChip` | `packages/views/layout/panel-nav.tsx`, `packages/views/chat/components/chat-bar.tsx` |
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

Testing motion/react in jsdom:

- Run tests that wait for an animation with real timers and `waitFor`. Motion's frame loop is shared by the whole test file: when it first starts under `vi.useFakeTimers()`, it stays stuck after `vi.useRealTimers()`, and later animations in that file never finish either. Keep fake timers to the reduced-motion branch, which does not animate.
- A `duration: 0` value reaches the element's style on Motion's next frame, not in the same commit. Wait a frame before asserting it.
- jsdom never applies discrete values from `transitionEnd`, such as `overflow` or `display`. Check those in the real app.

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
