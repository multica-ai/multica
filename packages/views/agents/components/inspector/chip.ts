/**
 * Shared trigger styling for inspector pickers (Runtime / Model / Visibility /
 * Concurrency).
 *
 * The defining choices:
 * - `rounded-md` — the shared row/menu radius, soft enough to feel like a
 *   button rather than a tab.
 * - `hover:bg-accent` — single hover layer carries the entire "this is a
 *   button" signal. We tried adding a hover-border on top, but layered hover
 *   states (border + bg) made the chip outline busier without adding info.
 * - `min-w-0` so children that `truncate` don't overflow the inspector's
 *   320px column.
 *
 * No default border on purpose: at rest the chip should sit quietly inside
 * the row; the moment the cursor enters, the bg flips and the affordance is
 * obvious.
 */
export const CHIP_CLASS =
  "group flex min-w-0 cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-0.5 text-caption transition-colors hover:bg-accent";

/**
 * Full-width form pickers (Runtime / Model / Thinking / Service tier) on the
 * agent page, agent creation and onboarding. They sit side by side in grids,
 * so every one shares this single-line height and surface; secondary detail
 * goes inline, never on a second line.
 */
export const FIELD_TRIGGER_CLASS =
  "flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-transparent px-3 text-left text-body transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50";

/** Read-only counterpart of `FIELD_TRIGGER_CLASS`. */
export const FIELD_STATIC_CLASS =
  "flex min-h-10 items-center gap-2 rounded-lg border border-input bg-input/50 px-3 text-body text-muted-foreground";
