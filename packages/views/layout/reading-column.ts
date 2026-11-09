/**
 * The reading column shared by the issue detail page and chat.
 *
 * The column widens with its host instead of parking at a fixed 896px: 70% of
 * the nearest `@container`, never below 56rem (the old cap, so laptop widths
 * are unchanged) and never above 80rem. Hosts mark that container; without one
 * `cqi` falls back to the viewport, which still lands inside the clamp.
 *
 * Only wide content takes the extra room. `--reading-measure` caps prose blocks
 * (see `prose.css`) at 52rem — the text width the old 896px column left after
 * its 32px gutters — so lines of text never get longer than they were, while
 * code, tables, logs and run cards span the full column.
 */
export const READING_COLUMN = "mx-auto w-full max-w-[clamp(56rem,70cqi,80rem)] [--reading-measure:52rem]";
