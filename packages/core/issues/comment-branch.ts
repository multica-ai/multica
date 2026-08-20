import type { AgentTask } from "../types";

export interface CommentBranchIntent {
  commentId: string;
  agentId?: string;
  contentBase: string;
}

export interface CommentBranchRequestState {
  fingerprint: string;
  requestId: string;
}

function commentBranchIntentFingerprint(intent: CommentBranchIntent): string {
  return JSON.stringify([
    intent.commentId,
    intent.agentId ?? null,
    intent.contentBase,
  ]);
}

/** Reuse a key while the outcome of the same logical request is unknown. */
export function ensureCommentBranchRequest(
  previous: CommentBranchRequestState | null,
  intent: CommentBranchIntent,
  createRequestId: () => string,
): CommentBranchRequestState {
  const fingerprint = commentBranchIntentFingerprint(intent);
  if (previous?.fingerprint === fingerprint) return previous;
  return { fingerprint, requestId: createRequestId() };
}

/** A transport failure or 5xx may have committed without returning a result. */
export function shouldRetainCommentBranchRequest(status?: number): boolean {
  return status === undefined || status >= 500;
}

/**
 * Index independent-run task provenance for comment rendering. A result
 * comment stores source_task_id, while the task stores the exact historical
 * branch point. Keeping this join in one pure helper makes Web and Mobile
 * render the same relation without duplicating it into comment storage.
 */
export function commentBranchPointsByTaskId(
  tasks: readonly AgentTask[],
  availableCommentIds?: ReadonlySet<string>,
): ReadonlyMap<string, string> {
  const result = new Map<string, string>();
  for (const task of tasks) {
    if (
      task.branch_point_comment_id &&
      (!availableCommentIds || availableCommentIds.has(task.branch_point_comment_id))
    ) {
      result.set(task.id, task.branch_point_comment_id);
    }
  }
  return result;
}
