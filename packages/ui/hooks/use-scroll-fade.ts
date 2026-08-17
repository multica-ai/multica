import { type RefObject, type CSSProperties, useEffect, useState, useCallback } from "react";

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

  const update = useCallback(() => {
    const el = ref.current;
    if (!el) return;

    const position = axis === "horizontal" ? el.scrollLeft : el.scrollTop;
    const scrollSize = axis === "horizontal" ? el.scrollWidth : el.scrollHeight;
    const clientSize = axis === "horizontal" ? el.clientWidth : el.clientHeight;
    const scrollable = scrollSize - clientSize;

    if (scrollable <= 0) {
      setFade("none");
      return;
    }

    const atStart = position <= 1;
    const atEnd = position >= scrollable - 1;

    if (atStart && atEnd) setFade("none");
    else if (atStart) setFade("end");
    else if (atEnd) setFade("start");
    else setFade("both");
  }, [axis, ref]);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const frame = requestAnimationFrame(update);

    el.addEventListener("scroll", update, { passive: true });
    const ro = new ResizeObserver(update);
    ro.observe(el);
    // ResizeObserver only fires on the container's own box. When children
    // grow inside a flex/auto-sized parent, the scroll extent can change while
    // the viewport does not — the mask would stay "none" until the user scrolls.
    // MutationObserver on childList catches those content insertions.
    const mo = new MutationObserver(update);
    mo.observe(el, { childList: true, subtree: true });

    return () => {
      cancelAnimationFrame(frame);
      el.removeEventListener("scroll", update);
      ro.disconnect();
      mo.disconnect();
    };
  }, [ref, update]);

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
