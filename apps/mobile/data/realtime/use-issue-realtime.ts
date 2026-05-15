/**
 * Per-issue realtime subscriptions. Mounted by the issue detail screen
 * with the active issue id; cleans up on navigate-away.
 *
 * Handles:
 *   - issue:updated / issue:deleted / issue_labels:changed → detail cache
 *   - comment:created / comment:updated / comment:deleted → timeline
 *   - activity:created → timeline
 *   - reaction:added / reaction:removed → comment reactions on timeline
 *   - issue_reaction:added / issue_reaction:removed → issue-level reactions on detail
 *   - task:queued / task:dispatch / task:progress / task:completed /
 *     task:failed / task:cancelled → invalidate timeline + detail (task
 *     state can flip an issue's status server-side without firing
 *     issue:updated, so we refetch the authoritative detail too)
 *   - reconnect → invalidate detail + timeline (we might've missed events
 *     while disconnected; server has no replay buffer for this client)
 *
 * Mobile pattern (per the realtime plan, see
 * /Users/qingnaiyuan/.claude/plans/plan-api-indexed-waffle.md):
 *   - Patch over invalidate where the payload contains the full object
 *   - Event always wins on optimistic-update conflicts; brief flicker
 *     is acceptable, correctness wins.
 *   - All handlers self-gate on `issue_id === issueId` so we only react
 *     to events for the currently-viewed issue.
 */
import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type {
  ActivityCreatedPayload,
  CommentCreatedPayload,
  CommentDeletedPayload,
  CommentUpdatedPayload,
  IssueDeletedPayload,
  IssueLabelsChangedPayload,
  IssueReactionAddedPayload,
  IssueReactionRemovedPayload,
  IssueUpdatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
  TaskCancelledPayload,
  TaskCompletedPayload,
  TaskDispatchPayload,
  TaskFailedPayload,
  TaskMessagePayload,
  TaskQueuedPayload,
} from "@multica/core/types";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useWSClient } from "./realtime-provider";
import {
  addCommentReaction,
  addIssueReaction,
  appendTimelineEntry,
  clearIssueDetail,
  commentToTimelineEntry,
  patchIssueDetail,
  patchIssueLabels,
  patchMyIssuesList,
  patchTimelineEntry,
  removeCommentReaction,
  removeFromMyIssuesList,
  removeIssueReaction,
  removeTimelineEntry,
} from "./issue-ws-updaters";

type TaskEventPayload =
  | TaskQueuedPayload
  | TaskDispatchPayload
  | TaskCompletedPayload
  | TaskFailedPayload
  | TaskCancelledPayload
  | TaskMessagePayload;

export function useIssueRealtime(
  issueId: string | undefined,
  onDeleted?: () => void,
) {
  const ws = useWSClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const qc = useQueryClient();

  useEffect(() => {
    if (!ws || !wsId || !issueId) return;

    const invalidateThisIssue = () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
    };

    const onTaskEvent = (p: unknown) => {
      if ((p as TaskEventPayload).issue_id !== issueId) return;
      invalidateThisIssue();
    };

    const unsubs: Array<() => void> = [
      // ----- Issue-level -----
      ws.on("issue:updated", (p) => {
        const payload = p as IssueUpdatedPayload;
        if (payload.issue.id !== issueId) return;
        patchIssueDetail(qc, wsId, payload.issue);
        patchMyIssuesList(qc, wsId, payload.issue);
      }),
      ws.on("issue:deleted", (p) => {
        const payload = p as IssueDeletedPayload;
        if (payload.issue_id !== issueId) return;
        clearIssueDetail(qc, wsId, issueId);
        removeFromMyIssuesList(qc, wsId, issueId);
        onDeleted?.();
      }),
      ws.on("issue_labels:changed", (p) => {
        const payload = p as IssueLabelsChangedPayload;
        if (payload.issue_id !== issueId) return;
        patchIssueLabels(qc, wsId, issueId, payload.labels);
      }),

      // ----- Comments / activity -----
      ws.on("comment:created", (p) => {
        const payload = p as CommentCreatedPayload;
        if (payload.comment.issue_id !== issueId) return;
        appendTimelineEntry(
          qc,
          wsId,
          issueId,
          commentToTimelineEntry(payload.comment),
        );
      }),
      ws.on("comment:updated", (p) => {
        const payload = p as CommentUpdatedPayload;
        if (payload.comment.issue_id !== issueId) return;
        const entry = commentToTimelineEntry(payload.comment);
        patchTimelineEntry(
          qc,
          wsId,
          issueId,
          (e) => e.type === "comment" && e.id === payload.comment.id,
          () => entry,
        );
      }),
      ws.on("comment:deleted", (p) => {
        const payload = p as CommentDeletedPayload;
        if (payload.issue_id !== issueId) return;
        removeTimelineEntry(
          qc,
          wsId,
          issueId,
          (e) => e.type === "comment" && e.id === payload.comment_id,
        );
      }),
      ws.on("activity:created", (p) => {
        const payload = p as ActivityCreatedPayload;
        if (payload.issue_id !== issueId) return;
        appendTimelineEntry(qc, wsId, issueId, payload.entry);
      }),

      // ----- Comment reactions -----
      ws.on("reaction:added", (p) => {
        const payload = p as ReactionAddedPayload;
        if (payload.issue_id !== issueId) return;
        addCommentReaction(
          qc,
          wsId,
          issueId,
          payload.reaction.comment_id,
          payload.reaction,
        );
      }),
      ws.on("reaction:removed", (p) => {
        const payload = p as ReactionRemovedPayload;
        if (payload.issue_id !== issueId) return;
        removeCommentReaction(
          qc,
          wsId,
          issueId,
          payload.comment_id,
          payload.emoji,
          payload.actor_id,
        );
      }),

      // ----- Issue-level reactions -----
      ws.on("issue_reaction:added", (p) => {
        const payload = p as IssueReactionAddedPayload;
        if (payload.issue_id !== issueId) return;
        addIssueReaction(qc, wsId, issueId, payload.reaction);
      }),
      ws.on("issue_reaction:removed", (p) => {
        const payload = p as IssueReactionRemovedPayload;
        if (payload.issue_id !== issueId) return;
        removeIssueReaction(
          qc,
          wsId,
          issueId,
          payload.emoji,
          payload.actor_id,
        );
      }),

      // ----- Agent task progress -----
      ws.on("task:queued", onTaskEvent),
      ws.on("task:dispatch", onTaskEvent),
      ws.on("task:progress", onTaskEvent),
      ws.on("task:completed", onTaskEvent),
      ws.on("task:failed", onTaskEvent),
      ws.on("task:cancelled", onTaskEvent),

      // ----- Reconnect -----
      ws.onReconnect(invalidateThisIssue),
    ];

    return () => {
      for (const unsub of unsubs) unsub();
    };
  }, [ws, wsId, issueId, qc, onDeleted]);
}
