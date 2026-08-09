"use client";

import {
  useCallback,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
} from "react";
import type { Editor } from "@tiptap/core";
import { snapWidthPct } from "./resize-magnet";

// FIR-4699 Phase 5 — the four corner handles for an inline image. Aspect ratio
// stays locked because we only ever set width (height is auto), and width is a
// percentage of the text column so it survives the comment rail opening. The
// snapping brain is the tested snapWidthPct; this file is the pointer wiring
// (browser-verified in Phase 8).

type Corner = "nw" | "ne" | "sw" | "se";

const CORNERS: Corner[] = ["nw", "ne", "sw", "se"];

// Right-side handles grow the image to the right (anchor = left edge); left-side
// handles grow it to the left (anchor = right edge). Either way the new width is
// the horizontal distance from the anchored edge.
function growsRight(corner: Corner): boolean {
  return corner === "ne" || corner === "se";
}

export function ImageResizeControls({
  editor,
  figureRef,
}: {
  editor: Editor;
  figureRef: RefObject<HTMLElement | null>;
}) {
  const startDrag = useCallback(
    (corner: Corner) => (e: ReactPointerEvent) => {
      e.preventDefault();
      e.stopPropagation();
      const figure = figureRef.current;
      // The editable surface is the text column the percentage is measured against.
      const column = editor.view.dom as HTMLElement;
      if (!figure || !column) return;
      const columnWidth = column.clientWidth;
      const rect = figure.getBoundingClientRect();
      const anchorX = growsRight(corner) ? rect.left : rect.right;
      const handle = e.currentTarget as HTMLElement;
      handle.setPointerCapture(e.pointerId);

      const onMove = (ev: PointerEvent) => {
        const widthPx = growsRight(corner)
          ? ev.clientX - anchorX
          : anchorX - ev.clientX;
        const pct = snapWidthPct((widthPx / columnWidth) * 100, columnWidth);
        editor.chain().setImageWidthPct(pct).run();
      };
      const onUp = () => {
        handle.releasePointerCapture(e.pointerId);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
      };
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
    },
    [editor, figureRef],
  );

  return (
    <>
      {CORNERS.map((corner) => (
        <span
          key={corner}
          className={`image-resize-handle image-resize-handle-${corner}`}
          data-corner={corner}
          onPointerDown={startDrag(corner)}
          // The handle is a drag affordance, not a text caret target.
          contentEditable={false}
        />
      ))}
    </>
  );
}
