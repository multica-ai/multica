/**
 * Mobile-owned project status + priority config. Mirror of
 * `packages/core/projects/config.ts` — same enum order, same labels, same
 * semantic colors. Mirrored (not imported) so mobile keeps full control of
 * Tailwind tokens (we use the mobile tailwind palette, web/desktop use v4
 * tokens with different class names like `text-warning`).
 *
 * Behavioral parity (apps/mobile/CLAUDE.md "Behavioral parity"):
 *   - Status enum order is identical to web. All 5 values render — `cancelled`
 *     is NOT hidden.
 *   - Priority enum order is identical to web. `none` renders as "No
 *     priority", not as an absence.
 *   - Labels are the canonical English strings; i18n lands later when
 *     mobile picks an i18n lib (web uses i18next).
 */
import type { ProjectPriority, ProjectStatus } from "@multica/core/types";

export const PROJECT_STATUSES: ProjectStatus[] = [
  "planned",
  "in_progress",
  "paused",
  "completed",
  "cancelled",
];

export const PROJECT_PRIORITIES: ProjectPriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

export const PROJECT_STATUS_LABEL: Record<ProjectStatus, string> = {
  planned: "Planned",
  in_progress: "In Progress",
  paused: "Paused",
  completed: "Completed",
  cancelled: "Cancelled",
};

export const PROJECT_PRIORITY_LABEL: Record<ProjectPriority, string> = {
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
  none: "No priority",
};

// Single hex per status, used by the SVG status icon (NativeWind classes
// can't be read by Svg props at runtime). Matches the semantic intent of
// the web tokens: planned/paused/cancelled are muted, in_progress is amber,
// completed is blue.
export const PROJECT_STATUS_COLOR: Record<ProjectStatus, string> = {
  planned: "#71717a",
  in_progress: "#f59e0b",
  paused: "#71717a",
  completed: "#3b82f6",
  cancelled: "#a1a1aa",
};

// Bar count for the priority icon (mirrors web's PROJECT_PRIORITY_CONFIG.bars).
export const PROJECT_PRIORITY_BARS: Record<ProjectPriority, number> = {
  urgent: 4,
  high: 3,
  medium: 2,
  low: 1,
  none: 0,
};

// Fallback for unknown server values per "Enum drift downgrades, not crashes"
// (root CLAUDE.md "API Response Compatibility"). Returns a sensible default
// so a future enum value still renders a labelled chip.
export function projectStatusLabel(value: string): string {
  return (PROJECT_STATUS_LABEL as Record<string, string>)[value] ?? value;
}

export function projectPriorityLabel(value: string): string {
  return (PROJECT_PRIORITY_LABEL as Record<string, string>)[value] ?? value;
}

export function projectStatusColor(value: string): string {
  return (PROJECT_STATUS_COLOR as Record<string, string>)[value] ?? "#71717a";
}

export function projectPriorityBars(value: string): number {
  return (PROJECT_PRIORITY_BARS as Record<string, number>)[value] ?? 0;
}
