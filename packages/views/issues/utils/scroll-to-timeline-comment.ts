import type { VirtuosoHandle } from "react-virtuoso";

/** Reveal a nested anchor before aligning it against measured reply content. */
export function scrollToTimelineComment({
  container, virtualizer, index, commentId, offset, onMounted, onComplete,
}: {
  container: HTMLElement;
  virtualizer: VirtuosoHandle | null;
  index: number;
  commentId: string;
  /** Comment top relative to the viewport; omitted for centered deep links. */
  offset?: number;
  onMounted?: () => void;
  onComplete?: () => void;
}) {
  let rafId = 0;
  let mountFrames = 0;
  let frames = 0;
  let last = -1;
  let cancelled = false;
  let mounted = false;
  const alignTarget = (el: HTMLElement, reveal = false) => {
    const c = container.getBoundingClientRect();
    const e = el.getBoundingClientRect();
    // A tall reply must first enter the observer's range, even when the saved
    // position lies thousands of pixels below its still-unmeasured top.
    const viewportOffset = reveal || offset === undefined ? (container.clientHeight - e.height) / 2 : offset;
    const target = Math.max(0, container.scrollTop + e.top - c.top - viewportOffset);
    const row = virtualizer ? el.closest<HTMLElement>("[data-item-index]") : null;
    if (row) {
      // Redirect the list's pending size corrections to the nested reply.
      virtualizer?.scrollToIndex({
        index, align: "start", offset: e.top - row.getBoundingClientRect().top - viewportOffset,
      });
    }
    // Native scrollIntoView also scrolls ancestors and can move the desktop
    // shell offscreen (#3929). Only the timeline owns this scroll.
    container.scrollTop = target;
    return target;
  };
  const land = () => {
    const el = container.querySelector<HTMLElement>(`[id="comment-${commentId}"]`);
    if (!el || el.dataset.virtualThreadMounted === "false") {
      if (el) alignTarget(el, true);
      if (++mountFrames < 60) rafId = requestAnimationFrame(land);
      else onComplete?.();
      return;
    }
    if (!mounted) { mounted = true; onMounted?.(); }
    const align = () => {
      const target = alignTarget(el);
      if (Math.abs(target - last) > 1 && ++frames < 30) {
        last = target;
        rafId = requestAnimationFrame(align);
      } else onComplete?.();
    };
    rafId = requestAnimationFrame(align);
  };
  const queueLanding = () => {
    if (cancelled) return;
    cancelAnimationFrame(rafId);
    rafId = requestAnimationFrame(land);
  };
  virtualizer?.scrollIntoView({ index, align: "start", done: queueLanding });
  // Reply bodies can resize the thread before Virtuoso calls done.
  queueLanding();
  return () => { cancelled = true; cancelAnimationFrame(rafId); };
}
