"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { autoUpdate, computePosition, flip, hide, offset, shift } from "@floating-ui/dom";

/** 只读选区沿用任务描述中 EditorBubbleMenu 的定位方式。 */
export function TextSelectionBubble({
  range, source, children, owner, markerRanges, onPositioned, keepVisibleWhenDetached = false, markerZIndex = 50,
}: {
  range: Range;
  source: HTMLElement;
  children: ReactNode;
  owner: string;
  markerRanges?: Range[];
  /** 浮层定位完成且可见后调用。 */
  onPositioned?: () => void;
  keepVisibleWhenDetached?: boolean;
  markerZIndex?: number;
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
      // 原文被实时更新移除时，保留正在编辑的批注，方便用户取回内容。
      if (keepVisibleWhenDetached && (!source.isConnected || !range.startContainer.isConnected)) return;
      // 根据原文选区判断可见性，避免把滚动区域外的批注挤到视口边缘。
      // 标记只调整横向位置，纵向仍跟随各自的原文。
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
    // 侧栏动画和字体重排可能移动选区，却不会触发元素或窗口尺寸变化。
    const cleanup = autoUpdate(anchor, element, update, { animationFrame: true });
    return () => { active = false; cleanup(); };
  }, [range, source, marker, markerRanges, onPositioned, keepVisibleWhenDetached]);

  return createPortal(
    <div ref={ref} data-reply-annotation-overlay={owner} data-editor-overlay-owner={owner}
      style={{ position: "fixed", zIndex: marker ? markerZIndex : 50, visibility: "hidden", width: "max-content" }}>
      {children}
    </div>,
    document.body,
  );
}
