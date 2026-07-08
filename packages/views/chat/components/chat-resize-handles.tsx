"use client";

import React from "react";

type DragDir = "left" | "top" | "corner";

interface ChatResizeHandlesProps {
  onDragStart: (e: React.PointerEvent, dir: DragDir) => void;
}

export function ChatResizeHandles({ onDragStart }: ChatResizeHandlesProps) {
  return (
    <>
      {/* Left edge — expands width when dragged left */}
      <div
        aria-hidden
        onPointerDown={(e) => onDragStart(e, "left")}
        className="absolute left-0 top-4 bottom-0 w-1 z-10 cursor-col-resize"
      />
      {/* Top edge — expands height when dragged up */}
      <div
        aria-hidden
        onPointerDown={(e) => onDragStart(e, "top")}
        className="absolute top-0 left-4 right-0 h-1 z-10 cursor-row-resize"
      />
      {/* Top-left corner — expands both width and height */}
      <div
        aria-hidden
        onPointerDown={(e) => onDragStart(e, "corner")}
        className="absolute top-0 left-0 size-4 z-20 cursor-nw-resize"
      />
    </>
  );
}
