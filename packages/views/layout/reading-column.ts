/**
 * The reading column shared by the issue detail page and chat.
 *
 * The column widens with its host instead of parking at a fixed 896px: 70% of
 * the nearest `@container`, never below 56rem (the old cap, so laptop widths
 * are unchanged) and never above 70rem. Hosts mark that container; without one
 * `cqi` falls back to the viewport, which still lands inside the clamp.
 *
 * Everything in the column shares its edges, prose included. The upper bound
 * is what keeps lines of text readable, so raise it with that in mind.
 */
export const READING_COLUMN = "mx-auto w-full max-w-[clamp(56rem,70cqi,70rem)]";
