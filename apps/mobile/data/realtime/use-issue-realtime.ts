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
import { useQueryClient } from "@tanstack/react-query";
import type {
  TaskCancelledPayload,
  TaskCompletedPayload,
  TaskDispatchPayload,
  TaskFailedPayload,
  TaskMessagePayload,
  TaskQueuedPayload,
} from "@multica/core/types";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
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
  removeCommentCascade,
  removeCommentReaction,
  removeFromMyIssuesList,
  removeIssueReaction,
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
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      if (!issueId) return;

      const invalidateThisIssue = () => {
        qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
        qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
      };

      // Task-query invalidation — separate from detail/timeline so the
      // AgentActivityRow + RunsSheet can refresh without forcing a full
      // timeline rebuild. WS task payloads only carry { task_id, agent_id,
      // issue_id, status } — not the full AgentTask object — so per
      // apps/mobile/CLAUDE.md "Patch over invalidate" rule #1 (payload is
      // just an id), invalidate is the correct primitive.
      const invalidateTaskQueries = () => {
        qc.invalidateQueries({ queryKey: issueKeys.activeTasks(wsId, issueId) });
        qc.invalidateQueries({ queryKey: issueKeys.tasks(wsId, issueId) });
      };

      // Shared cross-event handler for the 6 task:* subscriptions below.
      // All task events DO carry `issue_id` server-side, but `task:progress`
      // has no formal payload interface yet (WSEventPayloadMap entry is
      // `unknown`), so we cast through the union of the typed ones. If a new
      // task event lands without `issue_id`, this cast is the failure point —
      // grep here.
      const onTaskEvent = (p: unknown) => {
        if ((p as TaskEventPayload).issue_id !== issueId) return;
        invalidateThisIssue();
        invalidateTaskQueries();
      };

      return [
        // ----- Issue-level -----
        ws.on("issue:updated", (payload) => {
          if (payload.issue.id !== issueId) return;
          patchIssueDetail(qc, wsId, payload.issue);
          patchMyIssuesList(qc, wsId, payload.issue);
        }),
        ws.on("issue:deleted", (payload) => {
          if (payload.issue_id !== issueId) return;
          clearIssueDetail(qc, wsId, issueId);
          removeFromMyIssuesList(qc, wsId, issueId);
          onDeleted?.();
        }),
        ws.on("issue_labels:changed", (payload) => {
          if (payload.issue_id !== issueId) return;
          patchIssueLabels(qc, wsId, issueId, payload.labels);
        }),

        // ----- Comments / activity -----
        ws.on("comment:created", (payload) => {
          if (payload.comment.issue_id !== issueId) return;
          appendTimelineEntry(
            qc,
            wsId,
            issueId,
            commentToTimelineEntry(payload.comment),
          );
        }),
        ws.on("comment:updated", (payload) => {
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
        // Resolve / unresolve broadcast from any client. Payload carries the
        // full Comment with the new resolved_at/resolved_by_* fields, so we
        // can in-place-replace the entry via commentToTimelineEntry — no
        // refetch. Without these handlers the resolved state only updated
        // via the local mutation or via reconnect invalidate (the second
        // costs a full timeline refetch and busts every CommentCard memo).
        ws.on("comment:resolved", (payload) => {
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
        ws.on("comment:unresolved", (payload) => {
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
        ws.on("comment:deleted", (payload) => {
          if (payload.issue_id !== issueId) return;
          // Cascade: descendant replies must come out alongside the parent,
          // otherwise buildTimelineRows promotes them to top-level rows and
          // the user sees ghost replies after another client deletes the
          // thread. Server already cascades; this mirrors it in the cache.
          removeCommentCascade(qc, wsId, issueId, payload.comment_id);
        }),
        ws.on("activity:created", (payload) => {
          if (payload.issue_id !== issueId) return;
          appendTimelineEntry(qc, wsId, issueId, payload.entry);
        }),

        // ----- Comment reactions -----
        ws.on("reaction:added", (payload) => {
          if (payload.issue_id !== issueId) return;
          addCommentReaction(
            qc,
            wsId,
            issueId,
            payload.reaction.comment_id,
            payload.reaction,
          );
        }),
        ws.on("reaction:removed", (payload) => {
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
        ws.on("issue_reaction:added", (payload) => {
          if (payload.issue_id !== issueId) return;
          addIssueReaction(qc, wsId, issueId, payload.reaction);
        }),
        ws.on("issue_reaction:removed", (payload) => {
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
        ws.onReconnect(() => {
          invalidateThisIssue();
          invalidateTaskQueries();
        }),
      ];
    },
    [issueId, qc, onDeleted],
  );
}
