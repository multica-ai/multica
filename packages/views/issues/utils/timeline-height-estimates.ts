import type { TimelineEntry } from "@multica/core/types";
import type { CommentRun } from "../components/comment-runs";
import { timelineRowContent, type TimelineFolds, type TimelineRowItem } from "./timeline-row-content";

/**
 * Estimated rendered height of each timeline row, for Virtuoso's
 * `heightEstimates` (MUL-9538).
 *
 * Without them Virtuoso sizes every unmeasured row like the first one it
 * measures — on an issue page usually a 30px activity line, while a thread is
 * thousands of pixels. Opening a notification deep in a long issue then
 * jumps to "row 32 × 30px", fills the viewport with the dozens of threads it
 * believes fit there, measures them, and only then finds the target: one main
 * thread task that mounted most of the history. Estimates from content put
 * the jump near the target, so only its neighbourhood mounts.
 *
 * Constants were fitted against rendered comments (14px body text, 22.75px
 * lines plus paragraph spacing): within about ±10% for prose, lists, tables
 * and code. Being off only costs extra render passes, never correctness:
 * measured heights replace estimates as rows render.
 */

const LINE = 26;
const CODE_LINE = 21;
const FENCE = 16;
const TABLE_ROW = 41;
const HEADING = 34;
const BLOCK_GAP = 12;
/** Average glyph width of body text, for turning the column width into characters per line. */
const CHAR_WIDTH = 7.5;
/** The reading column's horizontal chrome: card padding and the comment's indent. */
const COLUMN_INSET = 98;
/** Body width at which the reading column stops growing. */
const MAX_BODY_WIDTH = 800;

const ACTIVITY_LINE = 30;
const ACTIVITY_ENTRY = 28;
const RESOLVED_BAR = 56;
const COLLAPSED_THREAD = 70;
const THREAD_FRAME = 30;
const THREAD_ROW = 75;
const RUN_BLOCK = 70;

/** Characters per rendered line for a timeline in a scroll container `width` px wide. */
export function timelineCharsPerLine(width: number): number {
  return Math.max(30, Math.round(Math.min(width - COLUMN_INSET, MAX_BODY_WIDTH) / CHAR_WIDTH));
}

const cache = new Map<string, number>();
const MAX_CACHED = 4_000;

/** Rendered height of a Markdown body, from its lines. Cached per content and width. */
export function estimateMarkdownHeight(markdown: string, charsPerLine: number): number {
  const key = `${charsPerLine}:${markdown}`;
  const cached = cache.get(key);
  if (cached !== undefined) return cached;
  let height = 0;
  let inFence = false;
  let blockStart = true;
  for (const line of markdown.split("\n")) {
    const text = line.trim();
    if (/^(`{3,}|~{3,})/.test(text)) {
      inFence = !inFence;
      height += FENCE;
      continue;
    }
    if (inFence) {
      height += CODE_LINE;
      continue;
    }
    if (!text) {
      blockStart = true;
      continue;
    }
    if (blockStart) height += BLOCK_GAP;
    blockStart = false;
    if (text.startsWith("|")) {
      if (!/^\|?\s*:?-+/.test(text)) height += TABLE_ROW;
    } else if (text.startsWith("#")) {
      height += HEADING;
    } else {
      height += Math.max(1, Math.ceil(text.length / charsPerLine)) * LINE;
    }
  }
  cache.set(key, height);
  if (cache.size > MAX_CACHED) cache.delete(cache.keys().next().value!);
  return height;
}

export function estimateTimelineItemHeights(
  items: readonly TimelineRowItem[],
  threadReplies: ReadonlyMap<string, readonly TimelineEntry[]>,
  commentRuns: ReadonlyMap<string, readonly CommentRun[]>,
  folds: TimelineFolds,
  options: {
    charsPerLine: number;
    /** Lines an activity block shows; 0 when it is folded to its count. */
    activityLines: (itemId: string) => number;
  },
): number[] {
  return items.map((item) => {
    const content = timelineRowContent(item, threadReplies, commentRuns, folds);
    switch (content.shape) {
      case "activity":
        return ACTIVITY_LINE + options.activityLines(item.id) * ACTIVITY_ENTRY;
      case "resolved-bar":
        return RESOLVED_BAR;
      case "collapsed-thread":
        return COLLAPSED_THREAD;
      case "thread":
        return content.bodies.reduce(
          (sum, body) => sum + THREAD_ROW + estimateMarkdownHeight(body.markdown, options.charsPerLine),
          THREAD_FRAME,
        );
      case "run":
        return content.bodies.reduce(
          (sum, body) => sum + estimateMarkdownHeight(body.markdown, options.charsPerLine),
          RUN_BLOCK,
        );
    }
  });
}
