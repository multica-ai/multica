import { useCallback, useEffect, useState } from "react";

/**
 * Drives the issue timeline's "Back to top" affordance — the mirror of
 * `useJumpToLatest` (MUL-8324) for the other end of a long conversation.
 *
 * `visible` is on while the reader is more than one viewport below the top
 * of the page; see `shouldShow`. Like "Jump to latest", this runs on every
 * platform: the ThreadMinimap rail jumps between comment threads, but the
 * top of the document — title, description, properties — is not a thread,
 * and on mobile there is no rail at all.
 *
 * `toTop` drives scrollTop to 0 directly instead of asking Virtuoso for the
 * first index: with `customScrollParent`, writing scrollTop is what makes
 * Virtuoso materialize the head rows. Unlike the bottom, the top never
 * moves while late content lands (a resolving image grows the content
 * below it, never the position of scrollTop 0), so there is no follow
 * window to keep re-pinning.
 */
export function useBackToTop(scrollContainerEl: HTMLElement | null) {
  const [visible, setVisible] = useState(false);

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
    // The viewport can resize without any scroll (panel drag, window
    // resize), which moves the one-viewport threshold past the reader.
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    observer?.observe(scrollContainerEl);
    return () => {
      scrollContainerEl.removeEventListener("scroll", schedule);
      observer?.disconnect();
      if (frame !== 0) cancelAnimationFrame(frame);
    };
  }, [scrollContainerEl]);

  const toTop = useCallback(() => {
    const el = scrollContainerEl;
    if (!el) return;
    el.scrollTop = 0;
  }, [scrollContainerEl]);

  return { visible, toTop };
}

/**
 * Show only once the reader has scrolled past one full viewport: near the
 * top the affordance would sit beside the very thing it jumps to, and a
 * timeline shorter than two viewports can never get that far.
 */
function shouldShow(el: HTMLElement) {
  return el.scrollTop > el.clientHeight;
}
