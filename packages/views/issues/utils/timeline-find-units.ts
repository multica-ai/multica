import type { TimelineEntry } from "@multica/core/types";
import { isDeletedComment } from "@multica/core/issues/comment-deletion";
import { redactSecrets } from "../../common/task-transcript/redact";
import {
  commentRunOutput,
  isRunFailureNotice,
  orderVisibleThreadRows,
  type CommentRun,
} from "../components/comment-runs";
import { deriveThreadResolution } from "../components/thread-utils";
import { markdownSearchText } from "./markdown-search-text";

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

/** The fields of a timeline row this needs; issue-detail's rows satisfy it. */
export type TimelineFindItem =
  | { kind: "run"; id: string; run: CommentRun; entry?: TimelineEntry }
  | { kind: "comment" | "resolved-bar"; id: string; entry: TimelineEntry }
  | { kind: "activity-group"; id: string };

/** What the reader has folded: resolved threads they opened, threads they collapsed. */
export interface TimelineFolds {
  expandedResolved: ReadonlySet<string>;
  collapsed: ReadonlySet<string>;
}

export function runOutputFindKey(taskId: string): string {
  return `run:${taskId}`;
}

/**
 * Every comment and run output the timeline shows, in reading order, whether
 * or not its row is mounted. In-page find counts these instead of rendering
 * the history (MUL-9538). It mirrors what renders: a folded or collapsed
 * thread contributes only what stays on screen, as the bar's matches always
 * did. Activity lines, names and timestamps are not searched.
 */
export function buildTimelineFindUnits(
  items: readonly TimelineFindItem[],
  threadReplies: ReadonlyMap<string, readonly TimelineEntry[]>,
  commentRuns: ReadonlyMap<string, readonly CommentRun[]>,
  folds: TimelineFolds,
): TimelineFindUnit[] {
  const units: TimelineFindUnit[] = [];
  const add = (key: string, itemId: string, anchorId: string, markdown: string | null | undefined) => {
    if (!markdown) return;
    const text = markdownSearchText(markdown).toLowerCase();
    if (text.trim()) units.push({ key, itemId, anchorId, text });
  };
  const addRunOutput = (run: CommentRun, itemId: string, anchorId: string) => {
    // A run that replied renders the reply instead of its output.
    const output = run.hasReply ? null : commentRunOutput(run.task);
    if (output) add(runOutputFindKey(run.task.id), itemId, anchorId, redactSecrets(output));
  };
  // A thread card (CommentCard): its root's body, then its open rows.
  const addThread = (itemId: string, root: TimelineEntry) => {
    if (folds.collapsed.has(root.id)) return;
    const replies = [...(threadReplies.get(root.id) ?? [])];
    const runs = commentRuns.get(root.id) ?? [];
    // A failure notice heading its thread shows as the run's block instead.
    const notice = runs.some((run) => run.hasReply && run.commentId === root.id && isRunFailureNotice(root, run.task));
    if (!isDeletedComment(root) && !notice) add(root.id, itemId, itemId, root.content);
    const resolution = deriveThreadResolution(root, replies);
    const foldedTo = resolution.kind === "reply" && !folds.expandedResolved.has(root.id) ? resolution.resolutionId : null;
    for (const row of orderVisibleThreadRows(root, replies, runs, foldedTo)) {
      if (!("run" in row)) {
        if (!isDeletedComment(row)) add(row.id, itemId, row.id, row.content);
        continue;
      }
      const reply = row.reply && !isDeletedComment(row.reply) && !isRunFailureNotice(row.reply, row.run.task)
        ? row.reply : undefined;
      if (reply) add(reply.id, itemId, reply.id, reply.content);
      else addRunOutput(row.run, itemId, row.run.task.id);
    }
  };

  for (const item of items) {
    if (item.kind === "comment") addThread(item.id, item.entry);
    if (item.kind !== "run") continue;
    const reply = item.entry;
    if (!reply) {
      addRunOutput(item.run, item.id, item.id);
      continue;
    }
    // Folded behind its resolution bar.
    if (reply.resolved_at && !folds.expandedResolved.has(reply.id)) continue;
    // A notice nobody replied to shows as the run's block, without output.
    if (isRunFailureNotice(reply, item.run.task)
      && !(threadReplies.get(reply.id) ?? []).some((entry) => !isDeletedComment(entry))) continue;
    addThread(item.id, reply);
  }
  return units;
}
