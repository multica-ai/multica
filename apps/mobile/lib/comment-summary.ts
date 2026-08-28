/**
 * Collapsed-root summary for the mobile timeline (RUYI-28).
 *
 * Pure function — no React, no RN. Keeps the truncation rules testable
 * outside a component tree (same lane as `stripMarkdown` / `timeline-thread`).
 *
 * Rules (approved scope):
 *   1. Markdown cleanup reuses `stripMarkdown` (mention links → label,
 *      images → 📷, plain links → label, blank-line runs collapsed).
 *   2. Cap at 120 **Unicode code points** — not UTF-16 units. Emoji /
 *      CJK-ext characters count once each; slicing by `.length` would cut
 *      a surrogate pair in half and render a tofu box at the seam.
 *   3. Ellipsis "…" is appended after truncation and does NOT count toward
 *      the cap (a string already ≤120 cp passes through unchanged).
 *   4. At most 2 lines — the line cut runs on the CAPPED string so a
 *      first line that already fills the budget can't resurrect a fragment
 *      of line 2.
 */
import { stripMarkdown } from "./strip-markdown";

export const COMMENT_SUMMARY_MAX_CODE_POINTS = 120;
export const COMMENT_SUMMARY_MAX_LINES = 2;

function countCodePoints(s: string): number {
  // Array.from iterates by code point (surrogate pairs yield one entry).
  return Array.from(s).length;
}

export function commentSummary(content: string | null | undefined): string {
  const cleaned = stripMarkdown(content ?? "");
  if (!cleaned) return "";

  let out = cleaned;
  if (countCodePoints(out) > COMMENT_SUMMARY_MAX_CODE_POINTS) {
    out = `${Array.from(out)
      .slice(0, COMMENT_SUMMARY_MAX_CODE_POINTS)
      .join("")}…`;
  }
  const lines = out.split("\n");
  if (lines.length > COMMENT_SUMMARY_MAX_LINES) {
    out = lines.slice(0, COMMENT_SUMMARY_MAX_LINES).join("\n");
  }
  return out;
}
