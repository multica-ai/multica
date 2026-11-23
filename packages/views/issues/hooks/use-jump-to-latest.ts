import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Drives the issue timeline's "Jump to latest" affordance (MUL-8324, MUL-8951,
 * MUL-9303).
 *
 * `away` is on while the newest entry is more than one screen away, and stays
 * on until the reader is within half a screen of the bottom, so the button
 * does not flicker when the reader rests near the threshold or late content
 * nudges the page. `visible` is `away`, plus — with `nearTopOnly` (mobile) —
 * the reader still being near the top of the page; see `isNearTop`.
 *
 * `tailPending` says the timeline's last row has not been rendered yet. The
 * virtualized timeline estimates every row it has not rendered at the height
 * of the first row it measured, usually a one-line activity row, so
 * `scrollHeight` can claim the bottom is close while screens of comments are
 * still below. With the tail unrendered, at least the list's bottom overscan
 * and the composer are below the viewport, which counts as away; once the
 * tail renders, `scrollHeight` is exact below the viewport again.
 *
 * `jump` drives scrollTop to the bottom directly instead of asking Virtuoso
 * for the last index: with `customScrollParent`, writing scrollTop is what
 * makes Virtuoso materialize the tail rows, and those rows then grow as their
 * real heights (markdown, code, images) land. The rAF loop keeps re-pinning
 * the bottom for `JUMP_FOLLOW_MS`, or until the reader scrolls themselves.
 */
/** How long a jump keeps re-pinning the bottom while late content lands. */
export const JUMP_FOLLOW_MS = 2000;

export function useJumpToLatest(
  scrollContainerEl: HTMLElement | null,
  { nearTopOnly = false, tailPending = false }: { nearTopOnly?: boolean; tailPending?: boolean } = {},
) {
  const [state, setState] = useState(HIDDEN);
  const awayRef = useRef(false);
  const cancelJumpRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!scrollContainerEl) {
      awayRef.current = false;
      setState(HIDDEN);
      return;
    }
    let frame = 0;
    const measure = () => {
      frame = 0;
      const away = tailPending || isAway(scrollContainerEl, awayRef.current);
      awayRef.current = away;
      const visible = away && (!nearTopOnly || isNearTop(scrollContainerEl));
      setState((prev) => (prev.away === away && prev.visible === visible ? prev : { away, visible }));
    };
    const schedule = () => {
      if (frame === 0) frame = requestAnimationFrame(measure);
    };
    measure();
    scrollContainerEl.addEventListener("scroll", schedule, { passive: true });
    // Content height changes without a scroll event (rows measured, comments
    // streamed in, threads expanded), so observe the content too.
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    observer?.observe(scrollContainerEl);
    for (const child of Array.from(scrollContainerEl.children)) observer?.observe(child);
    return () => {
      scrollContainerEl.removeEventListener("scroll", schedule);
      observer?.disconnect();
      if (frame !== 0) cancelAnimationFrame(frame);
    };
  }, [scrollContainerEl, nearTopOnly, tailPending]);

  useEffect(() => () => cancelJumpRef.current?.(), []);

  const jump = useCallback(() => {
    const el = scrollContainerEl;
    if (!el) return;
    cancelJumpRef.current?.();

    // Follow the bottom for a fixed window rather than until it looks stable:
    // a tail image or code block can resolve after the list has already held
    // still for several frames, and quitting early lands the reader short of
    // the newest entry (MUL-8324 review). The reader taking over ends it.
    const deadline = performance.now() + JUMP_FOLLOW_MS;
    let frame = 0;
    let cancelled = false;
    const toBottom = () => {
      el.scrollTop = el.scrollHeight - el.clientHeight;
    };
    const tick = () => {
      if (cancelled) return;
      if (!el.isConnected || performance.now() >= deadline) {
        stop();
        return;
      }
      if (distanceFromBottom(el) > 1) toBottom();
      frame = requestAnimationFrame(tick);
    };

    // Pointer input lands on the scroller itself. Keys do not: clicking the
    // button unmounts it, focus falls back to <body>, and PageUp/arrows then
    // never pass through the scroller — so listen for keys on the document,
    // in the capture phase, for the duration of the follow.
    const userInputs = ["wheel", "touchstart", "pointerdown"] as const;
    const doc = el.ownerDocument;
    const stop = () => {
      cancelled = true;
      cancelAnimationFrame(frame);
      for (const type of userInputs) el.removeEventListener(type, stop);
      doc.removeEventListener("keydown", stop, true);
      if (cancelJumpRef.current === stop) cancelJumpRef.current = null;
    };
    for (const type of userInputs) el.addEventListener(type, stop, { passive: true });
    doc.addEventListener("keydown", stop, { capture: true, passive: true });
    cancelJumpRef.current = stop;

    toBottom();
    frame = requestAnimationFrame(tick);
  }, [scrollContainerEl]);

  return { visible: state.visible, away: state.away, jump };
}

const HIDDEN = { away: false, visible: false };

/**
 * Away once more than a screen is left below; short pages never are. Once
 * away, the reader has to come within `RETURN_WITHIN_SCREENS` of the bottom.
 */
function isAway(el: HTMLElement, wasAway: boolean) {
  const threshold = wasAway ? el.clientHeight * RETURN_WITHIN_SCREENS : el.clientHeight;
  return distanceFromBottom(el) > threshold;
}

/** How close to the bottom, in screens, counts as back at the latest entry. */
const RETURN_WITHIN_SCREENS = 0.5;

/**
 * The viewport's top edge is within the first `SHOW_WITHIN_TOP_RATIO` of the
 * page: on mobile the button covers the text column, so once the reader has
 * scrolled further on their own they are reading, not hunting for the latest
 * entry, and it gets out of the way.
 */
function isNearTop(el: HTMLElement) {
  return el.scrollTop <= el.scrollHeight * SHOW_WITHIN_TOP_RATIO;
}

/** Share of the page the reader can scroll through before the button hides. */
const SHOW_WITHIN_TOP_RATIO = 0.2;

function distanceFromBottom(el: HTMLElement) {
  return el.scrollHeight - el.scrollTop - el.clientHeight;
}
