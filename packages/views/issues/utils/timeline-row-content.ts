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

/** The fields of a timeline row this needs; issue-detail's rows satisfy it. */
export type TimelineRowItem =
  | { kind: "run"; id: string; run: CommentRun; entry?: TimelineEntry }
  | { kind: "comment"; id: string; entry: TimelineEntry }
  | { kind: "resolved-bar"; id: string; entry: TimelineEntry }
  | { kind: "activity-group"; id: string };

/** What the reader has folded: resolved threads they opened, threads they collapsed. */
export interface TimelineFolds {
  expandedResolved: ReadonlySet<string>;
  collapsed: ReadonlySet<string>;
}

/** A Markdown body a row renders: a comment, or the output of a run that posted none. */
export interface TimelineRowBody {
  /** The comment id, or {@link runOutputKey} for a run's output. */
  key: string;
  /** `comment-<anchorId>` brings the body into the DOM, as a deep link does. */
  anchorId: string;
  markdown: string;
}

/**
 * What a timeline row shows, from data, for work that must not mount it:
 * in-page find's index and the list's height estimates. It mirrors what
 * renders, so a folded or collapsed thread contributes only what stays on
 * screen.
 */
export type TimelineRowContent =
  | { shape: "activity" }
  | { shape: "resolved-bar" }
  | { shape: "collapsed-thread" }
  | { shape: "thread"; bodies: TimelineRowBody[] }
  | { shape: "run"; bodies: TimelineRowBody[] };

export function runOutputKey(taskId: string): string {
  return `run:${taskId}`;
}

function runOutputBody(run: CommentRun, anchorId: string): TimelineRowBody[] {
  // A run that replied renders the reply instead of its output.
  const output = run.hasReply ? null : commentRunOutput(run.task);
  return output ? [{ key: runOutputKey(run.task.id), anchorId, markdown: redactSecrets(output) }] : [];
}

// A thread card (CommentCard): its root's body, then its open rows.
function threadContent(
  itemId: string,
  root: TimelineEntry,
  threadReplies: ReadonlyMap<string, readonly TimelineEntry[]>,
  commentRuns: ReadonlyMap<string, readonly CommentRun[]>,
  folds: TimelineFolds,
): TimelineRowContent {
  if (folds.collapsed.has(root.id)) return { shape: "collapsed-thread" };
  const bodies: TimelineRowBody[] = [];
  const add = (key: string, anchorId: string, markdown: string | null | undefined) => {
    if (markdown) bodies.push({ key, anchorId, markdown });
  };
  const replies = [...(threadReplies.get(root.id) ?? [])];
  const runs = commentRuns.get(root.id) ?? [];
  // A failure notice heading its thread shows as the run's block instead.
  const notice = runs.some((run) => run.hasReply && run.commentId === root.id && isRunFailureNotice(root, run.task));
  if (!isDeletedComment(root) && !notice) add(root.id, itemId, root.content);
  const resolution = deriveThreadResolution(root, replies);
  const foldedTo = resolution.kind === "reply" && !folds.expandedResolved.has(root.id) ? resolution.resolutionId : null;
  for (const row of orderVisibleThreadRows(root, replies, runs, foldedTo)) {
    if (!("run" in row)) {
      if (!isDeletedComment(row)) add(row.id, row.id, row.content);
      continue;
    }
    const reply = row.reply && !isDeletedComment(row.reply) && !isRunFailureNotice(row.reply, row.run.task)
      ? row.reply : undefined;
    if (reply) add(reply.id, reply.id, reply.content);
    else bodies.push(...runOutputBody(row.run, row.run.task.id));
  }
  return { shape: "thread", bodies };
}

export function timelineRowContent(
  item: TimelineRowItem,
  threadReplies: ReadonlyMap<string, readonly TimelineEntry[]>,
  commentRuns: ReadonlyMap<string, readonly CommentRun[]>,
  folds: TimelineFolds,
): TimelineRowContent {
  if (item.kind === "activity-group") return { shape: "activity" };
  if (item.kind === "resolved-bar") return { shape: "resolved-bar" };
  if (item.kind === "comment") return threadContent(item.id, item.entry, threadReplies, commentRuns, folds);
  const reply = item.entry;
  if (!reply) return { shape: "run", bodies: runOutputBody(item.run, item.id) };
  // Folded behind its resolution bar.
  if (reply.resolved_at && !folds.expandedResolved.has(reply.id)) return { shape: "resolved-bar" };
  // A notice nobody replied to shows as the run's block, without output.
  if (isRunFailureNotice(reply, item.run.task)
    && !(threadReplies.get(reply.id) ?? []).some((entry) => !isDeletedComment(entry))) {
    return { shape: "run", bodies: [] };
  }
  return threadContent(item.id, reply, threadReplies, commentRuns, folds);
}
