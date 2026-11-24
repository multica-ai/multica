"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { usePresence } from "motion/react";
import { autoUpdate, computePosition, flip, hide, offset, shift } from "@floating-ui/dom";
import { prefersReducedMotion } from "@multica/core/appearance";
import { UI_EASE_OUT_CSS, UI_MOTION_DURATION } from "@multica/ui/lib/motion";

/** Match the description's EditorBubbleMenu geometry for readonly selections. */
export function CommentSelectionBubble({ range, source, children, owner, markerRanges, markerZIndex = 20, onPositioned, keepVisibleWhenDetached = false }: {
  range: Range;
  source: HTMLElement;
  children: ReactNode;
  owner: string;
  markerRanges?: Range[];
  markerZIndex?: number;
  /** Called after the popup has a visible, usable position. */
  onPositioned?: () => void;
  keepVisibleWhenDetached?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const marker = !!markerRanges;
  const [isPresent, safeToRemove] = usePresence();
  const wasVisibleRef = useRef(false);

  useEffect(() => {
    const element = ref.current;
    if (!element || !isPresent) return;
    let active = true;
    const anchor = {
      contextElement: source,
      getBoundingClientRect: () => range.getBoundingClientRect(),
    };
    let revision = 0;
    const update = () => {
      const currentRevision = ++revision;
      // Keep an open note recoverable if realtime removes or replaces its source.
      if (keepVisibleWhenDetached && (!source.isConnected || !range.startContainer.isConnected)) return;
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
        const isVisible = !(!source.isConnected || !range.startContainer.isConnected ||
          (rect.width === 0 && rect.height === 0) || middlewareData.hide?.referenceHidden);
        const becameVisible = isVisible && !wasVisibleRef.current;
        wasVisibleRef.current = isVisible;
        element.style.visibility = isVisible ? "visible" : "hidden";
        element.style.left = `${x}px`;
        element.style.top = `${y}px`;
        if (becameVisible && typeof element.animate === "function" && !prefersReducedMotion()) {
          const fromTransform = marker ? "translateX(-4px) scale(0.96)" : "translateY(4px) scale(0.96)";
          element.animate(
            [
              { opacity: 0, transform: fromTransform },
              { opacity: 1, transform: "translate(0, 0) scale(1)" },
            ],
            { duration: UI_MOTION_DURATION.fast * 1000, easing: UI_EASE_OUT_CSS },
          );
        }
        if (isVisible) onPositioned?.();
      });
    };
    // Sidebar transitions and font/content reflow can move a Range without
    // resizing its source element or triggering a window resize.
    const cleanup = autoUpdate(anchor, element, update, { animationFrame: true });
    return () => { active = false; cleanup(); };
  }, [range, source, marker, markerRanges, onPositioned, keepVisibleWhenDetached, isPresent]);

  useEffect(() => {
    if (isPresent) return;
    const element = ref.current;
    if (element && wasVisibleRef.current && typeof element.animate === "function" && !prefersReducedMotion()) {
      const toTransform = marker ? "translateX(-4px) scale(0.96)" : "translateY(4px) scale(0.96)";
      const anim = element.animate(
        [
          { opacity: 1, transform: "translate(0, 0) scale(1)" },
          { opacity: 0, transform: toTransform },
        ],
        { duration: UI_MOTION_DURATION.micro * 1000, easing: "cubic-bezier(0.4, 0, 1, 1)", fill: "forwards" },
      );
      let done = false;
      const finish = () => {
        if (done) return;
        done = true;
        safeToRemove();
      };
      anim.onfinish = finish;
      anim.oncancel = finish;
      return () => { anim.cancel(); };
    }
    safeToRemove();
    return undefined;
  }, [isPresent, marker, safeToRemove]);

  return createPortal(
    <div ref={ref} data-reply-annotation-overlay={owner} data-editor-overlay-owner={owner}
      style={{ position: "fixed", zIndex: marker ? markerZIndex : 50, visibility: "hidden", width: "max-content", pointerEvents: isPresent ? undefined : "none" }}>
      {children}
    </div>,
    document.body,
  );
}
