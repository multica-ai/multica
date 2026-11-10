import { useCallback, useState } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useWSEvent } from "@multica/core/realtime";
import type { CommentCreatedPayload, IssueUpdatedPayload } from "@multica/core/types";

export type InboxAction = "replied" | "updated";

/**
 * How the viewer last acted on the open issue: a reply, or a status or
 * assignee change, made here or on another device while it stays open.
 * Forgotten once another issue opens.
 */
export function useActedOnIssue(issueId: string | null): InboxAction | null {
  const userId = useAuthStore((s) => s.user?.id);
  const [acted, setActed] = useState<{ issueId: string; action: InboxAction } | null>(null);

  const onComment = useCallback(
    (payload: unknown) => {
      const comment = (payload as Partial<CommentCreatedPayload> | null)?.comment;
      if (!issueId || !userId || comment?.issue_id !== issueId) return;
      if (comment.author_type !== "member" || comment.author_id !== userId) return;
      setActed({ issueId, action: "replied" });
    },
    [issueId, userId],
  );
  useWSEvent("comment:created", onComment);

  const onIssue = useCallback(
    (payload: unknown, actorId?: string, actorType?: string) => {
      const update = payload as Partial<IssueUpdatedPayload> | null;
      if (!issueId || !userId || update?.issue?.id !== issueId) return;
      if (actorType !== "member" || actorId !== userId) return;
      if (!update.status_changed && !update.assignee_changed) return;
      setActed({ issueId, action: "updated" });
    },
    [issueId, userId],
  );
  useWSEvent("issue:updated", onIssue);

  return acted?.issueId === issueId ? acted.action : null;
}
