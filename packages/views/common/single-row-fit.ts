"use client";

import { useCallback, useLayoutEffect, useRef, useState } from "react";

/**
 * Fit calculator for a single-row toolbar that collapses its tail into a
 * trailing trigger (the view bar; reusable for any "N chips + overflow
 * menu" strip).
 *
 * The caller renders every candidate twice: once for real (slice to
 * `fitCount`) and once inside a hidden mirror row attached to
 * `measureRef` (absolutely positioned, `visibility: hidden`, same
 * classes). Natural widths are read off the mirror, so the fit answer
 * never depends on what is currently mounted for real — no feedback
 * loop between measurement and rendering.
 *
 * Rule: the longest prefix that fits alongside its trailing chrome wins.
 * `reserve` returns the chrome width for a candidate fit count, including
 * permanent menus even when every item fits. Resolve both from the same
 * measurement before updating state, so overflow controls cannot feed a
 * stale fit back into a later render.
 *
 * Remeasures on container resize (ResizeObserver) and after every
 * commit (labels/items change the mirror); state only updates when the
 * answer actually changes, so the extra passes are free.
 */
export function useSingleRowFit({
  count,
  gap,
  reserve,
}: {
  /** Number of candidate items (mirror children must match). */
  count: number;
  /** Horizontal gap between items in px (the container's `gap`). */
  gap: number;
  /** Trailing chrome width in px for a candidate number of fitting items. */
  reserve: (fitCount: number) => number;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const measureRef = useRef<HTMLDivElement | null>(null);
  const [fitCount, setFitCount] = useState(count);

  const recompute = useCallback(() => {
    const container = containerRef.current;
    const mirror = measureRef.current;
    if (!container || !mirror) return;
    const available = container.clientWidth;
    const widths = Array.from(mirror.children).map(
      (child) => (child as HTMLElement).offsetWidth,
    );

    let next = widths.length;
    let used =
      widths.reduce((sum, width) => sum + width, 0) + Math.max(0, next - 1) * gap;
    while (next > 0 && used + gap + reserve(next) > available) {
      next--;
      used -= widths[next]! + (next > 0 ? gap : 0);
    }
    setFitCount((current) => (current === next ? current : next));
  }, [gap, reserve]);

  // After every commit: the mirror just re-rendered with current labels.
  useLayoutEffect(() => {
    recompute();
  });

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => recompute());
    observer.observe(container);
    return () => observer.disconnect();
  }, [recompute]);

  // Never report more than exists (items can shrink between renders).
  return { containerRef, measureRef, fitCount: Math.min(fitCount, count) };
}
