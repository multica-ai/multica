import type { VirtuosoHandle } from "react-virtuoso";

/** About two seconds of frames: content above the list settles well within it. */
const REVEAL_LIST_MAX_FRAMES = 120;

/**
 * Frames each list has left. The budget belongs to the list, not to one call:
 * every commit that re-runs the caller's effect (a realtime comment, a
 * timeline refresh) restarts the reveal, and must not restart its limit.
 */
const revealFramesLeft = new WeakMap<HTMLElement, number>();

/**
 * Hold the virtualized timeline's top at the top of the viewport until the
 * caller sees the list report a measured height and cancels.
 *
 * A long description can put the whole list below the viewport, where
 * Virtuoso renders nothing and never measures, and a landing waits on that
 * measurement. One write is not enough: content above the list can still
 * change height right after it (an editor filling in, a page mounting twice
 * while its address is canonicalized), which leaves the list out of range
 * again and the reader parked in the description.
 */
export function revealTimelineList(container: HTMLElement, list: HTMLElement) {
  let rafId = 0;
  const reveal = () => {
    const left = revealFramesLeft.get(list) ?? REVEAL_LIST_MAX_FRAMES;
    if (left <= 0) return;
    revealFramesLeft.set(list, left - 1);
    container.scrollTop += list.getBoundingClientRect().top - container.getBoundingClientRect().top;
    rafId = requestAnimationFrame(reveal);
  };
  reveal();
  return () => cancelAnimationFrame(rafId);
}

/** Reveal a nested anchor before aligning it against measured reply content. */
export function scrollToTimelineComment({
  container, virtualizer, index, commentId, offset, onMounted, onComplete,
}: {
  container: HTMLElement;
  virtualizer: VirtuosoHandle | null;
  index: number;
  commentId: string;
  /** Comment top relative to the viewport; omitted to center comments that fit. */
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
    const thread = el.closest<HTMLElement>("[data-item-index]") ?? el.parentElement?.closest<HTMLElement>('[id^="comment-"]');
    const stickyBar = thread?.querySelector<HTMLElement>("[data-thread-sticky-bar]");
    const headerHeight = stickyBar && !el.contains(stickyBar) ? stickyBar.offsetHeight : 0;
    // Keep a tall comment's beginning visible below its enclosing sticky bar.
    // Centering its full height would put its header above the viewport.
    const centeredOffset = Math.max(16 + headerHeight, (container.clientHeight - e.height) / 2);
    const viewportOffset = reveal || offset === undefined ? centeredOffset : offset;
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
  const findAnchor = () => container.querySelector<HTMLElement>(`[id="comment-${commentId}"]`);
  const land = () => {
    const el = findAnchor();
    if (!el || el.dataset.virtualThreadMounted === "false") {
      if (el) alignTarget(el, true);
      if (++mountFrames < 60) rafId = requestAnimationFrame(land);
      else onComplete?.();
      return;
    }
    if (!mounted) { mounted = true; onMounted?.(); }
    const align = () => {
      // Look the anchor up again every frame. A commit between frames can
      // unmount or replace it (Virtuoso re-ranging rows while a realtime update
      // lands); a detached node measures as zero, which would scroll the
      // timeline to the top and pass for a settled landing (MUL-9068).
      const current = findAnchor();
      if (!current || current.dataset.virtualThreadMounted === "false") {
        if (++frames >= 30) { onComplete?.(); return; }
        if (!current) virtualizer?.scrollIntoView({ index, align: "start", done: queueLanding });
        queueLanding();
        return;
      }
      const target = alignTarget(current);
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
