"use client";

import { useCallback, useEffect, useRef } from "react";
import {
  clampSidebarWidth,
  useSidebarResize,
} from "@multica/ui/components/ui/sidebar";
import { useT } from "../i18n";

const RESIZE_DRAG_THRESHOLD = 2;

/**
 * A column's trailing edge: drag to resize, click to collapse. Every column
 * sized by `--sidebar-width` shares the one committed width. The preview
 * writes the width straight onto the panel element and commits once on
 * release, so a drag re-renders nothing until the pointer comes up.
 */
export function PanelResizeHandle({
  panelRef,
  onToggle,
}: {
  panelRef: React.RefObject<HTMLElement | null>;
  onToggle: () => void;
}) {
  const { t } = useT("ui");
  const { commitWidth } = useSidebarResize();
  const cancelRef = useRef<(() => void) | null>(null);
  const draggedRef = useRef(false);

  useEffect(() => () => cancelRef.current?.(), []);

  const onPointerDown = useCallback(
    (e: React.PointerEvent<HTMLButtonElement>) => {
      const panel = panelRef.current;
      if (e.button !== 0 || e.isPrimary === false || !panel) return;
      e.preventDefault();
      cancelRef.current?.();
      draggedRef.current = false;
      const handle = e.currentTarget;
      const pointerId = e.pointerId;
      const startX = e.clientX;
      const startWidth = clampSidebarWidth(panel.getBoundingClientRect().width);
      let latestWidth = startWidth;
      document.documentElement.setAttribute("data-sidebar-resizing", "true");

      const finish = (mode: "commit" | "cancel") => {
        document.removeEventListener("pointermove", onMove);
        document.removeEventListener("pointerup", onUp);
        document.removeEventListener("pointercancel", onCancel);
        window.removeEventListener("blur", onBlur);
        document.documentElement.removeAttribute("data-sidebar-resizing");
        if (mode === "commit" && draggedRef.current) commitWidth(latestWidth);
        panel.style.removeProperty("--sidebar-width");
        cancelRef.current = null;
        if (handle.hasPointerCapture?.(pointerId)) handle.releasePointerCapture?.(pointerId);
      };
      const onMove = (event: PointerEvent) => {
        if (event.pointerId !== pointerId) return;
        const delta = event.clientX - startX;
        if (!draggedRef.current && Math.abs(delta) < RESIZE_DRAG_THRESHOLD) return;
        draggedRef.current = true;
        const next = clampSidebarWidth(startWidth + delta);
        if (next === latestWidth) return;
        latestWidth = next;
        panel.style.setProperty("--sidebar-width", `${next}px`);
      };
      const onUp = (event: PointerEvent) => {
        if (event.pointerId === pointerId) finish("commit");
      };
      const onCancel = (event: PointerEvent) => {
        if (event.pointerId === pointerId) finish("cancel");
      };
      const onBlur = () => finish("cancel");

      document.addEventListener("pointermove", onMove);
      document.addEventListener("pointerup", onUp);
      document.addEventListener("pointercancel", onCancel);
      window.addEventListener("blur", onBlur);
      cancelRef.current = () => finish("cancel");
      handle.setPointerCapture?.(pointerId);
    },
    [commitWidth, panelRef],
  );

  return (
    <button
      type="button"
      data-slot="space-panel-resize"
      aria-label={t(($) => $.toggle_sidebar)}
      title={t(($) => $.toggle_sidebar)}
      tabIndex={-1}
      onPointerDown={onPointerDown}
      onClick={() => {
        if (!draggedRef.current) onToggle();
      }}
      className="absolute inset-y-0 -right-1 z-20 w-2 cursor-ew-resize touch-none after:absolute after:inset-y-0 after:left-1/2 after:w-px after:-translate-x-1/2 after:transition-colors hover:after:bg-ring/60"
    />
  );
}
