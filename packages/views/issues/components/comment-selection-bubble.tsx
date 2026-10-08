"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { autoUpdate, computePosition, flip, hide, offset, shift } from "@floating-ui/dom";

/** Match the description's EditorBubbleMenu geometry for readonly selections. */
export function CommentSelectionBubble({ range, source, children, owner, markerRanges, onPositioned }: {
  range: Range;
  source: HTMLElement;
  children: ReactNode;
  owner: string;
  markerRanges?: Range[];
  /** Called after the popup has a visible, usable position. */
  onPositioned?: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const marker = !!markerRanges;
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    let active = true;
    const anchor = {
      contextElement: source,
      getBoundingClientRect: () => range.getBoundingClientRect(),
    };
    let revision = 0;
    const update = () => {
      const currentRevision = ++revision;
      // Keep visibility tied to the real quote, not a zero-width point outside
      // the scroll container. Markers move horizontally but never clamp/flip
      // vertically: doing so piles unrelated annotations at the viewport edge.
      computePosition(anchor, element, {
        strategy: "fixed",
        placement: marker ? "right-start" : "top",
        middleware: markerRanges ? [
          offset(({ rects }) => {
            const sorted = markerRanges.map((item) => ({ item, top: item.getBoundingClientRect().top }))
              .sort((a, b) => a.top - b.top);
            let top = -Infinity;
            for (const item of sorted) {
              top = Math.max(top + 28, item.top);
              if (item.item === range) break;
            }
            return {
              mainAxis: source.getBoundingClientRect().right - (rects.reference.x + rects.reference.width) + 4,
              crossAxis: top - rects.reference.y,
            };
          }),
          shift({ padding: 8, mainAxis: false, crossAxis: true }), hide(),
        ] : [offset(8), flip({ padding: 8, altBoundary: true }), shift({ padding: 8, altBoundary: true }), hide()],
      }).then(({ x, y, middlewareData }) => {
        if (!active || currentRevision !== revision || !element.isConnected) return;
        const rect = range.getBoundingClientRect();
        element.style.visibility = !source.isConnected || !range.startContainer.isConnected ||
          (rect.width === 0 && rect.height === 0) || middlewareData.hide?.referenceHidden ? "hidden" : "visible";
        element.style.left = `${x}px`;
        element.style.top = `${y}px`;
        if (element.style.visibility === "visible") onPositioned?.();
      });
    };
    // Sidebar transitions and font/content reflow can move a Range without
    // resizing its source element or triggering a window resize.
    const cleanup = autoUpdate(anchor, element, update, { animationFrame: true });
    return () => { active = false; cleanup(); };
  }, [range, source, marker, markerRanges, onPositioned]);

  return createPortal(
    <div ref={ref} data-reply-annotation-overlay={owner}
      style={{ position: "fixed", zIndex: marker ? 20 : 50, visibility: "hidden", width: "max-content" }}>
      {children}
    </div>,
    document.body,
  );
}
