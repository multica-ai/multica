"use client";

import { cn } from "@multica/ui/lib/utils";

const DRAG_TYPE = "application/x-multica-shape";

export interface NodePaletteProps {
  className?: string;
}

export function NodePalette({ className }: NodePaletteProps) {
  return (
    <div className={cn("flex flex-col gap-1.5 p-1.5 rounded-lg bg-card border shadow-sm", className)}>
      <div
        draggable
        role="button"
        tabIndex={0}
        aria-label="Rectangle"
        title="Rectangle"
        className="flex items-center justify-center w-9 h-9 rounded-md border border-border bg-muted/30 cursor-grab active:cursor-grabbing hover:bg-muted hover:border-primary/50 transition-colors text-muted-foreground hover:text-foreground"
        onDragStart={(e) => {
          e.dataTransfer.setData(DRAG_TYPE, "rectangle");
          e.dataTransfer.effectAllowed = "copy";
        }}
      >
        <svg width="24" height="18" viewBox="0 0 24 18">
          <rect x="1" y="1" width="22" height="16" rx="3" fill="none" stroke="currentColor" strokeWidth="1.5" />
        </svg>
      </div>
    </div>
  );
}
