import type { TimelineEntry } from "@multica/core/types";
import type { CommentRun } from "../components/comment-runs";
import { markdownSearchText } from "./markdown-search-text";
import { timelineRowContent, type TimelineFolds, type TimelineRowItem } from "./timeline-row-content";

/**
 * One searchable body in the issue timeline: a comment's Markdown, or the
 * output of a run that posted no comment. Its rendered root carries
 * `data-find-unit={key}`.
 */
export interface TimelineFindUnit {
  key: string;
  /** Timeline row (Virtuoso item) the body renders in. */
  itemId: string;
  /** `comment-<anchorId>` brings the body into the DOM, as a deep link does. */
  anchorId: string;
  /** Lower-cased rendered text (see {@link markdownSearchText}). */
  text: string;
}

/**
 * Every comment and run output the timeline shows, in reading order, whether
 * or not its row is mounted. In-page find counts these instead of rendering
 * the history (MUL-9538). It mirrors what renders: a folded or collapsed
 * thread contributes only what stays on screen, as the bar's matches always
 * did. Activity lines, names and timestamps are not searched.
 */
export function buildTimelineFindUnits(
  items: readonly TimelineRowItem[],
  threadReplies: ReadonlyMap<string, readonly TimelineEntry[]>,
  commentRuns: ReadonlyMap<string, readonly CommentRun[]>,
  folds: TimelineFolds,
): TimelineFindUnit[] {
  const units: TimelineFindUnit[] = [];
  for (const item of items) {
    const content = timelineRowContent(item, threadReplies, commentRuns, folds);
    if (content.shape !== "thread" && content.shape !== "run") continue;
    for (const body of content.bodies) {
      const text = markdownSearchText(body.markdown).toLowerCase();
      if (text.trim()) units.push({ key: body.key, itemId: item.id, anchorId: body.anchorId, text });
    }
  }
  return units;
}
