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
 * Rule: the longest prefix that fits alongside `reserve` px of trailing
 * chrome wins. `reserve` must cover everything that is ALWAYS rendered
 * after the items (triggers, menus) — it is subtracted even when every
 * item fits, otherwise a row that exactly fits its items clips the
 * chrome off the end.
 *
 * Remeasures on container resize (ResizeObserver) and after every
 * commit (labels/items change the mirror); state only updates when the
 * answer actually changes, so the extra passes are free.
 */
export function useSingleRowFit({
  count,
  gap,
  reserve,
  contentKey = "",
}: {
  /** Number of candidate items (mirror children must match). */
  count: number;
  /** Horizontal gap between items in px (the container's `gap`). */
  gap: number;
  /** Width in px to hold back for the overflow trigger. */
  reserve: number;
  /** Changes when the labels or ordered candidates in the mirror change. */
  contentKey?: string;
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

    let used = 0;
    let next = 0;
    for (let i = 0; i < widths.length; i++) {
      const withItem = used + (i > 0 ? gap : 0) + widths[i]!;
      if (withItem + gap + reserve > available) break;
      used = withItem;
      next = i + 1;
    }
    setFitCount((current) => (current === next ? current : next));
  }, [gap, reserve]);

  // Measure after the mirror changes. Running this after every commit caused a
  // synchronous layout-effect update on every table render; when the view bar
  // width was settling, React reported error #185 before the guarded setter
  // could converge. ResizeObserver covers geometry changes; this key covers
  // labels/order changes without creating a render-to-measure feedback loop.
  useLayoutEffect(() => {
    recompute();
  }, [contentKey, count, recompute]);

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
