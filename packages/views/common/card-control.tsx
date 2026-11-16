"use client";

import type { PointerEvent, ReactNode, SyntheticEvent } from "react";

const stopPropagation = (event: SyntheticEvent) => event.stopPropagation();

/**
 * Until the pointer is released, cancels native drags of any element that
 * contains `control` (the enclosing card link), leaving drags of the
 * control's own content alone.
 */
function blockEnclosingDrag(control: HTMLElement) {
  const doc = control.ownerDocument;
  const onDragStart = (event: DragEvent) => {
    if (event.target instanceof Node && event.target.contains(control)) event.preventDefault();
  };
  const release = () => {
    doc.removeEventListener("dragstart", onDragStart, true);
    doc.removeEventListener("pointerup", release, true);
    doc.removeEventListener("pointercancel", release, true);
  };
  doc.addEventListener("dragstart", onDragStart, true);
  doc.addEventListener("pointerup", release, true);
  doc.addEventListener("pointercancel", release, true);
}

/**
 * Wraps a control (picker, menu, button) that sits inside a card which is
 * itself a link and a drag source, so using the control neither follows the
 * link nor starts a drag. React events bubble through portals, so this also
 * covers clicks inside the control's open popup.
 *
 * Press events (`pointerdown` / `mousedown`) only stop propagation: they must
 * keep their default. Cancelling `pointerdown` makes the browser skip the
 * follow-up `mousedown`, and Base UI menus open on `mousedown`, so a
 * DropdownMenu inside would never open. With the press default kept, a press
 * that moves would natively drag the enclosing link, so that `dragstart` is
 * cancelled instead. `click` is cancelled so the enclosing link does not
 * navigate.
 */
export function CardControl({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={className}
      onPointerDown={(event: PointerEvent<HTMLDivElement>) => {
        event.stopPropagation();
        blockEnclosingDrag(event.currentTarget);
      }}
      onMouseDown={stopPropagation}
      onClick={(event) => {
        event.stopPropagation();
        event.preventDefault();
      }}
    >
      {children}
    </div>
  );
}
