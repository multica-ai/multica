"use client";

import type { Label } from "@multica/core/types";
import { X, Sparkles } from "lucide-react";

/**
 * Map a label's `#rrggbb` color to a readable text color.
 *
 * Uses the ITU-R BT.601 perceived-luminance formula: colors above the
 * threshold get dark text (#111827), colors below get light text (#f9fafb).
 * This works for both pastel and saturated palettes without a hard lookup
 * table.
 *
 * The malformed-hex fallback returns dark-on-default which is readable on
 * the default `backgroundColor` rendering path — better than pure black
 * which disappears on dark chips.
 *
 * SECURITY INVARIANT: `LabelChip` applies `style={{ backgroundColor: color }}`
 * directly, trusting the backend's color format. The backend's
 * `normalizeColor` regex pins the value to `^#?[0-9a-fA-F]{6}$`. If that regex
 * ever loosens (named colors, `url(...)`, etc.), this becomes an injection
 * vector.
 */
function contrastTextColor(hex: string): string {
  const h = hex.replace("#", "");
  if (h.length !== 6) return "#111827";
  const r = parseInt(h.slice(0, 2), 16) / 255;
  const g = parseInt(h.slice(2, 4), 16) / 255;
  const b = parseInt(h.slice(4, 6), 16) / 255;
  const luminance = 0.299 * r + 0.587 * g + 0.114 * b;
  return luminance > 0.55 ? "#111827" : "#f9fafb";
}

interface LabelChipProps {
  label: Label;
  onRemove?: () => void;
  className?: string;
  /**
   * When true, show the full label name without truncation. Use this in
   * management/edit surfaces where users need to see or verify the exact
   * name. The default (false) truncates at 12rem to keep chips compact in
   * the issue sidebar and future board/list card rows.
   */
  fullName?: boolean;
}

/**
 * Renders a single label as a colored pill. If `onRemove` is provided, shows
 * an × button that calls it. Used in the issue-detail sidebar, the picker,
 * and the management dialog.
 *
 * Labels with non-empty `instructions` get a Sparkles icon — a visual
 * "this label carries agent instructions" cue. Hovering the chip reveals
 * the instructions in the native title tooltip.
 */
export function LabelChip({ label, onRemove, className, fullName }: LabelChipProps) {
  const textColor = contrastTextColor(label.color);
  const nameClass = fullName ? "break-all" : "truncate max-w-[12rem]";
  const hasInstructions = Boolean(label.instructions && label.instructions.trim());
  // Tooltip + accessible label combine name + instructions so hover reveals
  // exactly what the agent will receive. Native `title` is used (not a
  // custom Tooltip component) so chips stay cheap to render anywhere.
  const tooltipText = hasInstructions
    ? `${label.name}\n\n📋 Agent instructions:\n${label.instructions}`
    : label.name;
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${className ?? ""}`}
      style={{ backgroundColor: label.color, color: textColor }}
      aria-label={tooltipText}
      title={tooltipText}
    >
      <span className={nameClass}>{label.name}</span>
      {hasInstructions && (
        <Sparkles
          className="h-2.5 w-2.5 shrink-0 opacity-90"
          strokeWidth={2.5}
          aria-hidden
        />
      )}
      {onRemove && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
          className="flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full hover:bg-current/20 focus:outline-none focus:ring-1 focus:ring-current"
          aria-label={`Remove label ${label.name}`}
        >
          <X className="h-2.5 w-2.5" strokeWidth={2.5} />
        </button>
      )}
    </span>
  );
}
