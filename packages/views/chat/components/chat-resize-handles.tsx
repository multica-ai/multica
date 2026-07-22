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
        className="absolute left-0 top-4 bottom-0 w-1 z-10 cursor-col-resize before:absolute before:inset-y-0 before:left-0 before:w-px before:bg-transparent before:transition-colors before:duration-100 hover:before:bg-foreground/20 active:before:bg-foreground/40 active:before:transition-none"
      />
      {/* Top edge — expands height when dragged up */}
      <div
        aria-hidden
        onPointerDown={(e) => onDragStart(e, "top")}
        className="absolute top-0 left-4 right-0 h-1 z-10 cursor-row-resize before:absolute before:inset-x-0 before:top-0 before:h-px before:bg-transparent before:transition-colors before:duration-100 hover:before:bg-foreground/20 active:before:bg-foreground/40 active:before:transition-none"
      />
      {/* Top-left corner — expands both width and height */}
      <div
        aria-hidden
        onPointerDown={(e) => onDragStart(e, "corner")}
        className="absolute top-0 left-0 size-4 z-20 cursor-nw-resize before:absolute before:left-1 before:top-1 before:size-2 before:border-l before:border-t before:border-transparent before:transition-colors before:duration-100 hover:before:border-foreground/20 active:before:border-foreground/40 active:before:transition-none"
      />
    </>
  );
}
