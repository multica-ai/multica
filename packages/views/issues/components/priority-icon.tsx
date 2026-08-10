import type { IssuePriority } from "@multica/core/types";
import { PRIORITY_CONFIG } from "@multica/core/issues/config";

/**
 * Priority glyphs, two families on purpose:
 * - low / medium / high share one three-bar frame (1 / 2 / 3 bars filled,
 *   the rest faint) so severity reads as a fill level at a glance;
 * - urgent breaks out of the scale entirely — a filled badge with an
 *   exclamation mark — because it is an interrupt, not "even higher".
 * - none stays a quiet dash.
 */
export function PriorityIcon({
  priority,
  className = "",
  inheritColor = false,
}: {
  priority: IssuePriority | string;
  className?: string;
  inheritColor?: boolean;
}) {
  const cfg = priority in PRIORITY_CONFIG ? PRIORITY_CONFIG[priority as IssuePriority] : null;

  // "none" — simple horizontal dash
  if (!cfg || priority === "none") {
    return (
      <svg
        viewBox="0 0 16 16"
        className={`h-3.5 w-3.5 ${inheritColor ? "" : "text-muted-foreground"} shrink-0 ${className}`}
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      >
        <line x1="3" y1="8" x2="13" y2="8" />
      </svg>
    );
  }

  if (priority === "urgent") {
    return (
      <svg
        viewBox="0 0 16 16"
        className={`h-3.5 w-3.5 ${inheritColor ? "" : cfg.color} shrink-0 ${className}`}
        fill="none"
      >
        <rect x="2" y="2" width="12" height="12" rx="3" fill="currentColor" />
        <line
          x1="8"
          y1="5"
          x2="8"
          y2="8.6"
          stroke="var(--background, #fff)"
          strokeWidth="1.7"
          strokeLinecap="round"
        />
        <circle cx="8" cy="11" r="0.95" fill="var(--background, #fff)" />
      </svg>
    );
  }

  // low / medium / high — a constant three-bar frame; severity = fill count.
  const filled = Math.min(cfg.bars, 3);
  const BAR_HEIGHTS = [6, 9, 12];
  return (
    <svg
      viewBox="0 0 16 16"
      className={`h-3.5 w-3.5 ${inheritColor ? "" : cfg.color} shrink-0 ${className}`}
      fill="currentColor"
    >
      {BAR_HEIGHTS.map((height, i) => (
        <rect
          key={i}
          x={2 + i * 4.25}
          y={14 - height}
          width="3.5"
          height={height}
          rx="1"
          opacity={i < filled ? 1 : 0.35}
        />
      ))}
    </svg>
  );
}
