"use client";

import { useCallback } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import type {
  ActivityCreatedPayload,
  Comment,
  CommentCreatedPayload,
  CommentDeletedPayload,
  CommentResolvedPayload,
  CommentUnresolvedPayload,
  CommentUpdatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
  TimelineEntry,
} from "../types";
import { useWSEvent, useWSReconnect } from "../realtime";
import { commentToTimelineEntry } from "./comment-thread";
import { removeCommentSubtree } from "./comment-deletion";
import { issueKeys } from "./queries";
import { sortTimelineEntriesAsc } from "./timeline-sort";

type TLCache = TimelineEntry[];

function acceptsCommentRevision(
  current: TimelineEntry,
  incoming: Comment,
): boolean {
  return (
    current.revision === undefined ||
    (incoming.revision !== undefined && incoming.revision >= current.revision)
  );
}

function applyCommentSnapshot(
  qc: QueryClient,
  issueId: string,
  comment: Comment,
) {
  let missingRevision = false;
  qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
    old?.map((entry) => {
      if (entry.id !== comment.id) return entry;
      if (entry.revision !== undefined && comment.revision === undefined) {
        missingRevision = true;
        return entry;
      }
      return acceptsCommentRevision(entry, comment)
        ? {
            ...commentToTimelineEntry(comment),
            actor_name: entry.actor_name,
            actor_avatar_url: entry.actor_avatar_url,
          }
        : entry;
    }),
  );
  if (missingRevision) {
    // During a rolling deployment, an old server may emit an unversioned
    // snapshot after this cache has already observed a versioned one. Never
    // overwrite the ordered value; refetch from the authoritative API instead.
    qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
  }
}

/**
 * Keeps an on-screen issue timeline live. `useRealtimeSync` only marks the
 * timeline stale (`refetchType: "none"`) and leaves mounted views to patch the
 * cache granularly, so every client that shows a timeline must mount this.
 */
export function useIssueTimelineSync(issueId: string) {
  const qc = useQueryClient();

  // Reconnect recovery: invalidate so the next render refetches the full
  // timeline. Cheaper than diffing across a possibly-long disconnect.
  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    }, [qc, issueId]),
  );

  useWSEvent(
    "comment:created",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentCreatedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) => {
          const entry = commentToTimelineEntry(comment);
          if (!old) return [entry];
          if (old.some((e) => e.id === comment.id)) return old;
          return sortTimelineEntriesAsc([...old, entry]);
        });
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "comment:updated",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUpdatedPayload;
        if (comment.issue_id !== issueId) return;
        applyCommentSnapshot(qc, issueId, comment);
      },
      [qc, issueId],
    ),
  );

  // Granular handlers for comment:resolved / comment:unresolved. The payload
  // carries the full Comment with the new resolved_at/resolved_by_* fields,
  // which `commentToTimelineEntry` already preserves, so the existing
  // entry can simply be replaced in place. Without these handlers the only
  // path that updated the cache was `useRealtimeSync`'s global invalidate,
  // which forces a full timeline refetch and busts every CommentCard memo.
  useWSEvent(
    "comment:resolved",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentResolvedPayload;
        if (comment.issue_id !== issueId) return;
        applyCommentSnapshot(qc, issueId, comment);
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "comment:unresolved",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUnresolvedPayload;
        if (comment.issue_id !== issueId) return;
        applyCommentSnapshot(qc, issueId, comment);
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "comment:deleted",
    useCallback(
      (payload: unknown) => {
        const { comment_id, issue_id } = payload as CommentDeletedPayload;
        if (issue_id !== issueId) return;
        // A comment with replies is tombstoned (comment:updated), never
        // removed, so any cached reply of a removed comment is stale: older
        // servers cascaded the delete to every descendant.
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old ? removeCommentSubtree(old, comment_id) : old,
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "activity:created",
    useCallback(
      (payload: unknown) => {
        const p = payload as ActivityCreatedPayload;
        if (p.issue_id !== issueId) return;
        const entry = p.entry;
        if (!entry || !entry.id) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) => {
          if (!old) return [entry];
          if (old.some((e) => e.id === entry.id)) return old;
          return sortTimelineEntriesAsc([...old, entry]);
        });
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id, comment_revision } = payload as ReactionAddedPayload;
        if (issue_id !== issueId) return;
        let missingRevision = false;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) => {
            if (e.id !== reaction.comment_id) return e;
            if (comment_revision === undefined && e.revision !== undefined) {
              missingRevision = true;
              return e;
            }
            if (
              comment_revision !== undefined &&
              e.revision !== undefined &&
              comment_revision < e.revision
            ) return e;
            const existing = e.reactions ?? [];
            if (existing.some((r) => r.id === reaction.id)) return e;
            return {
              ...e,
              revision: comment_revision ?? e.revision,
              reactions: [...existing, reaction],
            };
          }),
        );
        if (missingRevision) {
          qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
        }
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as ReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        let missingRevision = false;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) => {
            if (e.id !== p.comment_id) return e;
            if (p.comment_revision === undefined && e.revision !== undefined) {
              missingRevision = true;
              return e;
            }
            if (
              p.comment_revision !== undefined &&
              e.revision !== undefined &&
              p.comment_revision < e.revision
            ) return e;
            return {
              ...e,
              revision: p.comment_revision ?? e.revision,
              reactions: (e.reactions ?? []).filter(
                (r) =>
                  !(
                    r.emoji === p.emoji &&
                    r.actor_type === p.actor_type &&
                    r.actor_id === p.actor_id
                  ),
              ),
            };
          }),
        );
        if (missingRevision) {
          qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
        }
      },
      [qc, issueId],
    ),
  );
}
