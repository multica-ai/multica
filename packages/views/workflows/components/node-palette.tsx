"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import type { NodeShape } from "@multica/core/types";
import { NODE_SHAPES } from "@multica/core/types";

const SHAPE_LABELS: Record<NodeShape, string> = {
  rectangle: "Rectangle",
  diamond: "Diamond",
  pill: "Pill",
  hexagon: "Hexagon",
};

const SHAPE_ICONS: Record<NodeShape, React.ReactNode> = {
  rectangle: (
    <svg width="24" height="18" viewBox="0 0 24 18">
      <rect x="1" y="1" width="22" height="16" rx="3" fill="none" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  diamond: (
    <svg width="24" height="24" viewBox="0 0 24 24">
      <polygon points="12,1 23,12 12,23 1,12" fill="none" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  pill: (
    <svg width="28" height="18" viewBox="0 0 28 18">
      <rect x="1" y="1" width="26" height="16" rx="9" fill="none" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  hexagon: (
    <svg width="24" height="24" viewBox="0 0 24 24">
      <polygon points="7,1 17,1 23,12 17,23 7,23 1,12" fill="none" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  annotation: (
    <svg width="20" height="24" viewBox="0 0 20 24">
      <rect x="1" y="1" width="18" height="22" rx="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <line x1="4" y1="7" x2="16" y2="7" stroke="currentColor" strokeWidth="1" />
      <line x1="4" y1="11" x2="13" y2="11" stroke="currentColor" strokeWidth="1" />
      <line x1="4" y1="15" x2="15" y2="15" stroke="currentColor" strokeWidth="1" />
      <line x1="4" y1="19" x2="10" y2="19" stroke="currentColor" strokeWidth="1" />
    </svg>
  ),
};

const PALETTE_ITEMS = NODE_SHAPES.map((s) => ({ type: s as string, label: SHAPE_LABELS[s], icon: SHAPE_ICONS[s] }));

const DRAG_TYPE = "application/x-multica-shape";
const DRAG_COLOR_TYPE = "application/x-multica-color";

const PRESET_COLORS = [
  "#ef4444", "#f97316", "#eab308", "#22c55e", "#06b6d4",
  "#3b82f6", "#6366f1", "#8b5cf6", "#ec4899",
];

export interface NodePaletteProps {
  className?: string;
}

export function NodePalette({ className }: NodePaletteProps) {
  const [selectedColor, setSelectedColor] = useState<string>("");

  return (
    <div className={cn("flex flex-col gap-1.5 p-1.5 rounded-lg bg-card border shadow-sm", className)}>
      {/* Color picker row */}
      <div className="flex gap-1 items-center">
        <button
          type="button"
          onClick={() => setSelectedColor("")}
          className={cn(
            "w-5 h-5 rounded border transition-colors shrink-0",
            !selectedColor ? "border-primary ring-1 ring-primary" : "border-border hover:border-muted-foreground",
          )}
          title="Default"
        >
          <div className="w-full h-full rounded-[2px] bg-card border border-border" />
        </button>
        {PRESET_COLORS.map((c) => (
          <button
            key={c}
            type="button"
            onClick={() => setSelectedColor(c)}
            className={cn(
              "w-5 h-5 rounded border transition-colors shrink-0",
              selectedColor === c ? "border-primary ring-1 ring-primary" : "border-border hover:border-muted-foreground",
            )}
            title={c}
          >
            <div className="w-full h-full rounded-[2px]" style={{ backgroundColor: c }} />
          </button>
        ))}
      </div>

      {/* Shape icons */}
      <div className="flex gap-1.5">
        {PALETTE_ITEMS.map((item) => (
          <div
            key={item.type}
            draggable
            role="button"
            tabIndex={0}
            aria-label={item.label}
            title={item.label}
            className="flex items-center justify-center w-9 h-9 rounded-md border border-border bg-muted/30 cursor-grab active:cursor-grabbing hover:bg-muted hover:border-primary/50 transition-colors text-muted-foreground hover:text-foreground"
            onDragStart={(e) => {
              e.dataTransfer.setData(DRAG_TYPE, item.type);
              if (selectedColor) {
                e.dataTransfer.setData(DRAG_COLOR_TYPE, selectedColor);
              }
              e.dataTransfer.effectAllowed = "copy";
            }}
          >
            {item.icon}
          </div>
        ))}
      </div>
    </div>
  );
}
