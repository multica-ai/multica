import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Drives the issue timeline's "Jump to latest" affordance (MUL-8324).
 *
 * `visible` is on while the reader is still near the top of the page and the
 * newest entry is more than one screen away; see `shouldShow`.
 *
 * `jump` drives scrollTop to the bottom directly instead of asking Virtuoso
 * for the last index: with `customScrollParent`, writing scrollTop is what
 * makes Virtuoso materialize the tail rows, and those rows then grow as their
 * real heights (markdown, code, images) land. The rAF loop keeps re-pinning
 * the bottom for `JUMP_FOLLOW_MS`, or until the reader scrolls themselves.
 */
/** How long a jump keeps re-pinning the bottom while late content lands. */
export const JUMP_FOLLOW_MS = 2000;

export function useJumpToLatest(scrollContainerEl: HTMLElement | null) {
  const [visible, setVisible] = useState(false);
  const cancelJumpRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!scrollContainerEl) {
      setVisible(false);
      return;
    }
    let frame = 0;
    const measure = () => {
      frame = 0;
      setVisible(shouldShow(scrollContainerEl));
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
  }, [scrollContainerEl]);

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

  return { visible, jump };
}

/**
 * Show only near the top of the page: the viewport's top edge must still be
 * within the first `SHOW_WITHIN_TOP_RATIO` of the page, and more than one
 * screen must be left below. Once the reader has scrolled further on their
 * own they are reading, not hunting for the latest entry, so the button gets
 * out of the way; short pages never show it at all.
 */
function shouldShow(el: HTMLElement) {
  return (
    el.scrollTop <= el.scrollHeight * SHOW_WITHIN_TOP_RATIO &&
    distanceFromBottom(el) > el.clientHeight
  );
}

/** Share of the page the reader can scroll through before the button hides. */
const SHOW_WITHIN_TOP_RATIO = 0.2;

function distanceFromBottom(el: HTMLElement) {
  return el.scrollHeight - el.scrollTop - el.clientHeight;
}
