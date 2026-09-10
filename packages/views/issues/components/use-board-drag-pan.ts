import { useCallback, useEffect, useRef } from "react";

/** Threshold (px) that distinguishes a click from a pan drag. Mirrors the
 * board's dnd-kit PointerSensor `activationConstraint: { distance: 5 }` so a
 * blank-area press and a card press feel identical up to the moment one is
 * recognized as a drag. */
const PAN_ACTIVATION_DISTANCE = 5;

// Elements whose press must NOT start a board pan — they own their own pointer
// semantics (dnd-kit cards, links, form controls, menus). A gesture starting
// anywhere inside one of these is left untouched. Kept broad on purpose (P2-4):
// besides the obvious controls it covers label/checkbox/radio, ARIA link/menu/
// option roles, and elements that opt out via `data-no-board-pan`.
const INTERACTIVE_SELECTOR = [
  "[data-board-card]",
  "[data-no-board-pan]",
  "a",
  "button",
  "input",
  "textarea",
  "select",
  "option",
  "label",
  "summary",
  "[role='button']",
  "[role='link']",
  "[role='menuitem']",
  "[role='option']",
  "[role='checkbox']",
  "[role='radio']",
  "[role='tab']",
  "[role='switch']",
  "[contenteditable='true']",
].join(", ");

/**
 * Blank-area left-drag panning for a horizontally scrollable board (#6700,
 * Trello/Linear pattern): pressing the LEFT mouse button on empty board
 * background and dragging pans the board horizontally — the page follows the
 * cursor. Dragging a card still moves the card (dnd-kit); this hook stays out
 * of the way because it never activates when the gesture starts on a card or
 * any interactive element. Touch and pen input are intentionally left to the
 * browser; this PR only owns the left-mouse-button gesture.
 *
 * ### dnd-kit coexistence invariant (P2-5 — do not break)
 * These pointer handlers sit on the SAME scroll container that hosts the
 * `DndContext`. They are mutually exclusive with card dragging purely by
 * ORIGIN: `onPointerDown` bails whenever the press starts inside
 * `INTERACTIVE_SELECTOR`, and every draggable card root carries
 * `data-board-card` (see `DraggableBoardCard` in `board-card.tsx`). If a future
 * change removes that attribute from the card root, or nests the pan container
 * below the card listeners, card drags and board pans will start fighting.
 * Keep `data-board-card` on the card root and keep this exclusion in sync.
 *
 * ### Design notes
 *   - Pointer Events + pointer capture claimed on `pointerdown` (not deferred
 *     to first move), so every `pointermove` keeps dispatching to the container
 *     even after the pointer leaves it or the window. Deferring capture was the
 *     "scrolls back when the pointer exits the board" bug: without an early
 *     capture the browser ran a native selection-drag whose auto-scroll fought
 *     `scrollLeft`, and re-entering the container produced a jumped delta.
 *   - Text selection is suppressed from `pointerdown` (P1-3): `user-select:
 *     none` on the container plus `selectstart` / `dragstart` preventers, then
 *     restored in `reset`. Doing it on down (not after the 5px threshold) closes
 *     the window where Safari/Firefox would start a selection whose auto-scroll
 *     yanks the board back. Because selection can no longer begin, `pointermove`
 *     no longer needs a per-frame `removeAllRanges` (P3-7).
 *   - Mouse left button only (`event.pointerType === "mouse"` and
 *     `event.button === 0`). Touch/pen are untouched so native touch scrolling
 *     and pinch zoom stay browser-owned.
 *   - Activation is gated on a ~5px move so a plain click is not swallowed.
 *   - Panning uses the captured pointer's `clientX` deltas — never a hover
 *     target or `elementFromPoint` — so leaving the container can't lose track.
 *   - Horizontal axis only: `deltaY` is never read, `scrollTop` never written.
 *     `scrollLeft` is clamped to `[0, scrollWidth - clientWidth]` so reaching an
 *     edge stops cleanly instead of bouncing back.
 *   - Cleanup on `pointerup` / `pointercancel` / `lostpointercapture` and
 *     window `blur`, plus a `buttons` check on move, so a lost release cannot
 *     leave the board stuck in a panning state.
 *
 * Returns props to spread onto the scroll container.
 */
export function useBoardDragPan<T extends HTMLElement>() {
  const ref = useRef<T | null>(null);
  // Pointer id of the active/pending gesture, or null when idle.
  const pointerIdRef = useRef<number | null>(null);
  // True once the ~5px threshold is crossed and we are actually panning.
  const activeRef = useRef(false);
  const startXRef = useRef(0);
  const lastXRef = useRef(0);

  const beginSelectionSuppression = useCallback((el: T) => {
    el.style.userSelect = "none";
    el.style.setProperty("-webkit-user-select", "none");
  }, []);

  const reset = useCallback(() => {
    const el = ref.current;
    if (el && pointerIdRef.current !== null) {
      // releasePointerCapture throws if the capture was already lost; ignore.
      try {
        el.releasePointerCapture(pointerIdRef.current);
      } catch {
        /* capture already released */
      }
    }
    pointerIdRef.current = null;
    activeRef.current = false;
    if (el) {
      el.style.removeProperty("cursor");
      // Restore text selection once the gesture ends.
      el.style.removeProperty("user-select");
      el.style.removeProperty("-webkit-user-select");
    }
  }, []);

  const onPointerDown = useCallback((event: React.PointerEvent<T>) => {
    // Left mouse button only. Primary touch/pen also report button === 0, but
    // this board gesture is intentionally scoped to mouse input.
    if (event.pointerType !== "mouse" || event.button !== 0) return;
    const el = ref.current;
    if (!el) return;
    // Ignore gestures that begin on a card or interactive control — those
    // belong to dnd-kit / links / form fields (see coexistence invariant).
    const target = event.target as Element | null;
    if (target && target.closest(INTERACTIVE_SELECTOR)) return;

    pointerIdRef.current = event.pointerId;
    activeRef.current = false;
    startXRef.current = event.clientX;
    lastXRef.current = event.clientX;

    // Suppress text selection from the very first event (P1-3), before the 5px
    // threshold, so no selection can begin and auto-scroll the container.
    beginSelectionSuppression(el);

    // Claim the pointer immediately so subsequent moves keep coming to this
    // element even once the cursor leaves the board.
    try {
      el.setPointerCapture(event.pointerId);
    } catch {
      // Pointer capture unsupported/rejected (older engines, synthetic events).
      // The gesture still works: pointer events keep flowing to the container
      // while the button is held, and the window `blur` handler plus the
      // per-move `buttons` check below guarantee cleanup if a release is missed.
    }
    event.preventDefault();
  }, [beginSelectionSuppression]);

  const onPointerMove = useCallback((event: React.PointerEvent<T>) => {
    if (pointerIdRef.current === null || event.pointerId !== pointerIdRef.current) return;
    const el = ref.current;
    if (!el) return;

    // The primary button was released somewhere we didn't hear about (window
    // blur, drag out of the document). buttons bit 0 is the left button.
    if ((event.buttons & 1) === 0) {
      reset();
      return;
    }

    if (!activeRef.current) {
      if (Math.abs(event.clientX - startXRef.current) < PAN_ACTIVATION_DISTANCE) return;
      // Cross the threshold: begin panning. Pointer is already captured and
      // selection already suppressed from pointerdown; just show the affordance.
      activeRef.current = true;
      el.style.cursor = "grabbing";
    }

    // Horizontal axis only, driven by captured-pointer clientX deltas. Clamp to
    // the scrollable range so hitting an edge simply stops. (Selection was
    // suppressed on pointerdown, so no per-frame range clearing is needed.)
    const delta = event.clientX - lastXRef.current;
    lastXRef.current = event.clientX;
    const maxScroll = el.scrollWidth - el.clientWidth;
    const next = Math.min(Math.max(el.scrollLeft - delta, 0), Math.max(maxScroll, 0));
    el.scrollLeft = next;
    event.preventDefault();
  }, [reset]);

  const onPointerUp = useCallback((event: React.PointerEvent<T>) => {
    if (event.pointerId !== pointerIdRef.current) return;
    reset();
  }, [reset]);

  // Belt-and-suspenders selection/native-drag blockers while a gesture is
  // pending or active (P1-3): `user-select` covers most engines, but Safari and
  // Firefox can still start a selection or an image/text drag, so veto the
  // events outright until the gesture ends. Attached as native listeners
  // because React has no synthetic `selectstart` event.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const veto = (event: Event) => {
      if (pointerIdRef.current !== null) event.preventDefault();
    };
    el.addEventListener("selectstart", veto);
    el.addEventListener("dragstart", veto);
    return () => {
      el.removeEventListener("selectstart", veto);
      el.removeEventListener("dragstart", veto);
    };
  }, []);

  useEffect(() => {
    const handleBlur = () => reset();
    window.addEventListener("blur", handleBlur);
    return () => window.removeEventListener("blur", handleBlur);
  }, [reset]);

  return {
    ref,
    onPointerDown,
    onPointerMove,
    onPointerUp,
    onPointerCancel: onPointerUp,
    onLostPointerCapture: onPointerUp,
  };
}
