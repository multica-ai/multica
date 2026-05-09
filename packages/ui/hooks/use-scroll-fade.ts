import { type RefObject, type CSSProperties, useEffect, useState, useCallback } from "react";

/**
 * Returns a dynamic maskImage style based on scroll position.
 * - At top → fade bottom only
 * - At bottom → fade top only
 * - In middle → fade both
 * - No overflow → undefined (no mask)
 */
export function useScrollFade(
  ref: RefObject<HTMLElement | null>,
  fadeSize = 32
): CSSProperties | undefined {
  const [fade, setFade] = useState<"none" | "top" | "bottom" | "both">("none");

  const update = useCallback(() => {
    const el = ref.current;
    if (!el) return;

    const { scrollTop, scrollHeight, clientHeight } = el;
    const scrollable = scrollHeight - clientHeight;

    if (scrollable <= 0) {
      setFade("none");
      return;
    }

    const atTop = scrollTop <= 1;
    const atBottom = scrollTop >= scrollable - 1;

    if (atTop && atBottom) setFade("none");
    else if (atTop) setFade("bottom");
    else if (atBottom) setFade("top");
    else setFade("both");
  }, [ref]);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const frame = requestAnimationFrame(update);

    el.addEventListener("scroll", update, { passive: true });
    const ro = new ResizeObserver(update);
    ro.observe(el);

    return () => {
      cancelAnimationFrame(frame);
      el.removeEventListener("scroll", update);
      ro.disconnect();
    };
  }, [ref, update]);

  if (fade === "none") return undefined;

  const top = fade === "top" || fade === "both" ? `transparent 0%, black ${fadeSize}px` : "black 0%";
  const bottom =
    fade === "bottom" || fade === "both"
      ? `black calc(100% - ${fadeSize}px), transparent 100%`
      : "black 100%";

  const gradient = `linear-gradient(to bottom, ${top}, ${bottom})`;

  return {
    maskImage: gradient,
    WebkitMaskImage: gradient,
  };
}
