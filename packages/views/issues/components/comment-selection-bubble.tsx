"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { autoUpdate, computePosition, flip, hide, offset, shift } from "@floating-ui/dom";

/** Match the description's EditorBubbleMenu geometry for readonly selections. */
export function CommentSelectionBubble({ range, source, children, owner, markerRanges }: {
  range: Range;
  source: HTMLElement;
  children: ReactNode;
  owner: string;
  markerRanges?: Range[];
}) {
  const ref = useRef<HTMLDivElement>(null);
  const marker = !!markerRanges;
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    let active = true;
    const anchor = {
      contextElement: source,
      getBoundingClientRect: () => {
        const rect = range.getBoundingClientRect();
        if (!markerRanges) return rect;
        // Stack nearby source markers so two quotes on one line remain clickable.
        const sorted = markerRanges.map((item) => ({ item, top: item.getBoundingClientRect().top }))
          .sort((a, b) => a.top - b.top);
        let top = -Infinity;
        for (const item of sorted) {
          top = Math.max(top + 28, item.top);
          if (item.item === range) break;
        }
        return new DOMRect(source.getBoundingClientRect().right, top, 0, 24);
      },
    };
    const update = () => {
      computePosition(anchor, element, {
        strategy: "fixed",
        placement: marker ? "right-start" : "top",
        middleware: [offset(marker ? 4 : 8), flip(), shift({ padding: 8 }), hide()],
      }).then(({ x, y, middlewareData }) => {
        if (!active || !element.isConnected) return;
        element.style.visibility = !source.isConnected || middlewareData.hide?.referenceHidden ? "hidden" : "visible";
        element.style.left = `${x}px`;
        element.style.top = `${y}px`;
      });
    };
    const cleanup = autoUpdate(anchor, element, update);
    return () => { active = false; cleanup(); };
  }, [range, source, marker, markerRanges]);

  return createPortal(
    <div ref={ref} data-reply-annotation-overlay={owner}
      style={{ position: "fixed", zIndex: marker ? 20 : 50, visibility: "hidden", width: "max-content" }}>
      {children}
    </div>,
    document.body,
  );
}
