"use client";

import { useCallback, useEffect, useRef } from "react";
import {
  clampListColumnWidth,
  clampSidebarWidth,
  useSidebarResize,
} from "@multica/ui/components/ui/sidebar";
import { useT } from "../i18n";

const RESIZE_DRAG_THRESHOLD = 2;

/**
 * Which committed width a column is sized by: `nav` is `--sidebar-width`, the
 * space panel's; `list` is `--list-column-width`, shared by the list columns
 * whose rows need more room than the nav's labels (Inbox, Chat).
 */
export type PanelWidth = "nav" | "list";

const WIDTH_VARIABLE: Record<PanelWidth, string> = {
  nav: "--sidebar-width",
  list: "--list-column-width",
};

/**
 * A column's trailing edge: drag to resize, click to collapse unless
 * `resizeOnly` keeps the column expanded. Every column
 * sized by the same width variable shares the one committed width. The preview
 * writes the width straight onto the panel element and commits once on
 * release, so a drag re-renders nothing until the pointer comes up.
 */
export function PanelResizeHandle({
  panelRef,
  onToggle,
  width = "nav",
  resizeOnly = false,
}: {
  panelRef: React.RefObject<HTMLElement | null>;
  onToggle: () => void;
  width?: PanelWidth;
  /** Drag to resize without offering the shared navigation collapse action. */
  resizeOnly?: boolean;
}) {
  const { t } = useT("ui");
  const { commitWidth, commitListWidth } = useSidebarResize();
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
      const clamp = width === "list" ? clampListColumnWidth : clampSidebarWidth;
      const commit = width === "list" ? commitListWidth : commitWidth;
      const variable = WIDTH_VARIABLE[width];
      const handle = e.currentTarget;
      const pointerId = e.pointerId;
      const startX = e.clientX;
      const startWidth = clamp(panel.getBoundingClientRect().width);
      let latestWidth = startWidth;
      document.documentElement.setAttribute("data-sidebar-resizing", "true");

      const finish = (mode: "commit" | "cancel") => {
        document.removeEventListener("pointermove", onMove);
        document.removeEventListener("pointerup", onUp);
        document.removeEventListener("pointercancel", onCancel);
        window.removeEventListener("blur", onBlur);
        document.documentElement.removeAttribute("data-sidebar-resizing");
        if (mode === "commit" && draggedRef.current) commit(latestWidth);
        panel.style.removeProperty(variable);
        cancelRef.current = null;
        if (handle.hasPointerCapture?.(pointerId)) handle.releasePointerCapture?.(pointerId);
      };
      const onMove = (event: PointerEvent) => {
        if (event.pointerId !== pointerId) return;
        const delta = event.clientX - startX;
        if (!draggedRef.current && Math.abs(delta) < RESIZE_DRAG_THRESHOLD) return;
        draggedRef.current = true;
        const next = clamp(startWidth + delta);
        if (next === latestWidth) return;
        latestWidth = next;
        panel.style.setProperty(variable, `${next}px`);
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
    [commitListWidth, commitWidth, panelRef, width],
  );

  return (
    <button
      type="button"
      data-slot="space-panel-resize"
      aria-label={resizeOnly ? t(($) => $.resize_list_column) : t(($) => $.toggle_sidebar)}
      title={resizeOnly ? t(($) => $.resize_list_column) : t(($) => $.toggle_sidebar)}
      tabIndex={-1}
      onPointerDown={onPointerDown}
      onClick={() => {
        if (!resizeOnly && !draggedRef.current) onToggle();
      }}
      className="absolute inset-y-0 -right-1 z-20 w-2 cursor-ew-resize touch-none after:absolute after:inset-y-0 after:left-1/2 after:w-px after:-translate-x-1/2 after:transition-colors hover:after:bg-ring/60"
    />
  );
}
