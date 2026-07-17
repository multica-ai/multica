"use client";

import { ResizeHandle } from "@multica/ui/components/ui/resize-handle";
import type {
  ResizeAxis,
  ResizeDelta,
} from "@multica/ui/hooks/use-resize-gesture";
import { cn } from "@multica/ui/lib/utils";

import type { DragDir } from "./use-chat-resize";

interface ChatResizeHandlesProps {
  onResizeStart: () => void;
  onResize: (dir: DragDir, delta: ResizeDelta) => void;
  onResizeEnd: () => void;
}

// The window is anchored bottom-right, so only the left edge, top edge and
// top-left corner are draggable. Edge handles start past the corner's 16px box
// so the three hit areas never overlap; the indicator sits on the window edge
// itself rather than centred in the hit area.
const HANDLES: {
  dir: DragDir;
  axis: ResizeAxis;
  className: string;
}[] = [
  // Width/height come from the shared 8px grab zone; only the placement is
  // ours. The corner is the one hit area that is deliberately square.
  {
    dir: "left",
    axis: "x",
    className: "left-0 top-4 bottom-0 z-10 after:start-0",
  },
  {
    dir: "top",
    axis: "y",
    className: "top-0 left-4 right-0 z-10 after:top-0",
  },
  { dir: "corner", axis: "xy", className: "top-0 left-0 size-4 z-20" },
];

export function ChatResizeHandles({
  onResizeStart,
  onResize,
  onResizeEnd,
}: ChatResizeHandlesProps) {
  return (
    <>
      {HANDLES.map(({ dir, axis, className }) => (
        <ResizeHandle
          key={dir}
          aria-hidden
          axis={axis}
          className={cn("absolute", className)}
          onResizeStart={onResizeStart}
          onResize={(delta) => onResize(dir, delta)}
          onResizeEnd={onResizeEnd}
        />
      ))}
    </>
  );
}
