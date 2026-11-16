import { type RefObject, type CSSProperties, useEffect, useState } from "react";

export type ScrollFadeAxis = "vertical" | "horizontal";

/**
 * Returns a dynamic maskImage style based on scroll position.
 * - At start → fade end only
 * - At end → fade start only
 * - In middle → fade both
 * - No overflow → undefined (no mask)
 */
export function useScrollFade(
  ref: RefObject<HTMLElement | null>,
  fadeSize = 32,
  axis: ScrollFadeAxis = "vertical",
): CSSProperties | undefined {
  const [fade, setFade] = useState<"none" | "start" | "end" | "both">("none");

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    // Resizes and child mutations funnel through one rAF-coalesced run
    // (scroll stays synchronous — see its listener below). Each run reads
    // layout once and calls setFade only when the value actually changes:
    // with dozens of fade hosts on screen, a mount or a realtime-event
    // re-render used to fire hundreds of identical dispatchSetState calls
    // per second — every one of them went through React's lane
    // scheduler and stretched tab switches to seconds.
    let last: "none" | "start" | "end" | "both" | null = null;
    let raf = 0;
    const run = () => {
      // A synchronous scroll absorbs any queued frame: this measurement
      // covers it, and leaving the handle alive would both double-fire the
      // callback and survive unmount cleanup (which sees raf === 0).
      if (raf) {
        cancelAnimationFrame(raf);
        raf = 0;
      }
      const node = ref.current;
      if (!node) return;
      const position = axis === "horizontal" ? node.scrollLeft : node.scrollTop;
      const scrollSize = axis === "horizontal" ? node.scrollWidth : node.scrollHeight;
      const clientSize = axis === "horizontal" ? node.clientWidth : node.clientHeight;
      const scrollable = scrollSize - clientSize;
      const atStart = position <= 1;
      const atEnd = position >= scrollable - 1;
      const next: "none" | "start" | "end" | "both" =
        scrollable <= 0 || (atStart && atEnd) ? "none" : atStart ? "end" : atEnd ? "start" : "both";
      if (next !== last) {
        last = next;
        setFade(next);
      }
    };
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(run);
    };

    schedule();
    // Scroll stays synchronous on purpose. Consumer wiring treats a scroll
    // and its fade verdict as one task — chat's stick-to-bottom latch, for
    // one, attributes wheel displacement to the scroll that confirms it, and
    // deferring the fade update off that task reordered the handshake in a
    // way its tests (and real scrolling feedback) depend on not happening.
    // User scrolling is also far too rare to need coalescing; the storms come
    // from resizes and DOM mutations below.
    el.addEventListener("scroll", run, { passive: true });
    const ro = new ResizeObserver(schedule);
    ro.observe(el);
    // ResizeObserver only fires on the container's own box. When children
    // grow inside a flex/auto-sized parent, the scroll extent can change while
    // the viewport does not — the mask would stay "none" until the user scrolls.
    // MutationObserver on childList catches those content insertions.
    const mo = new MutationObserver(schedule);
    mo.observe(el, { childList: true, subtree: true });

    return () => {
      if (raf) cancelAnimationFrame(raf);
      el.removeEventListener("scroll", run);
      ro.disconnect();
      mo.disconnect();
    };
  }, [axis, ref]);

  if (fade === "none") return undefined;

  const start =
    fade === "start" || fade === "both"
      ? `transparent 0%, black ${fadeSize}px`
      : "black 0%";
  const end =
    fade === "end" || fade === "both"
      ? `black calc(100% - ${fadeSize}px), transparent 100%`
      : "black 100%";

  const direction = axis === "horizontal" ? "right" : "bottom";
  const gradient = `linear-gradient(to ${direction}, ${start}, ${end})`;

  return {
    maskImage: gradient,
    WebkitMaskImage: gradient,
  };
}
