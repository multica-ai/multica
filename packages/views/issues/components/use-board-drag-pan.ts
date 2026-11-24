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
 * Returns left-mouse drag handlers for horizontal panning of empty board space.
 * Exclude interactive elements and data-board-card roots so panning cannot compete
 * with dnd-kit card dragging. Keep that attribute on every draggable card root.
 *
 * Capture the pointer and suppress native selection on pointerdown; waiting for
 * the movement threshold lets native selection autoscroll fight scrollLeft.
 * The threshold still distinguishes a click from a pan. Use captured clientX
 * deltas and clamp horizontal bounds; leave touch and pen scrolling to the browser.
 * Reset on release, cancellation, lost capture, blur, or a missing pressed button.
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
