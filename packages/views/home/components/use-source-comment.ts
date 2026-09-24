"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import type { NeedsMeItem } from "@multica/core/home";
import { isDeletedComment } from "@multica/core/issues/comment-deletion";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import type { TimelineEntry } from "@multica/core/types";

/**
 * The comment a queue entry is about, shown verbatim. No summarizing: the
 * delivery card is the agent's own final comment, the blocked card is what it
 * wrote when it stopped, the mention card is the comment that named the
 * viewer. Returns null for entries with no issue (a failed quick create), and
 * while the timeline loads.
 */
export function pickSourceComment(
  item: NeedsMeItem,
  timeline: readonly TimelineEntry[],
  viewerId: string | undefined,
): TimelineEntry | null {
  const comments = timeline.filter(
    (entry) => entry.type === "comment" && !isDeletedComment(entry) && !!entry.content?.trim(),
  );
  if (comments.length === 0) return null;

  if (item.kind === "mentioned") {
    const commentId = item.inbox?.details?.comment_id;
    const exact = commentId ? comments.find((entry) => entry.id === commentId) : undefined;
    if (exact) return exact;
  }

  const latestBy = (predicate: (entry: TimelineEntry) => boolean) => {
    for (let i = comments.length - 1; i >= 0; i--) {
      const entry = comments[i]!;
      if (predicate(entry)) return entry;
    }
    return null;
  };

  const actor = item.actor;
  const actorIsViewer = actor?.type === "member" && actor.id === viewerId;
  if (actor && actor.type !== "system" && !actorIsViewer) {
    const byActor = latestBy((entry) => entry.actor_type === actor.type && entry.actor_id === actor.id);
    if (byActor) return byActor;
  }
  // Assignee is the viewer (or a squad): the newest comment someone else wrote.
  return latestBy((entry) => !(entry.actor_type === "member" && entry.actor_id === viewerId));
}

export function useSourceComment(item: NeedsMeItem | null, enabled = true) {
  const viewerId = useAuthStore((s) => s.user?.id);
  const issueId = item?.issueId ?? "";
  const { data: timeline, isLoading } = useQuery({
    ...issueTimelineOptions(issueId),
    enabled: enabled && !!issueId,
  });
  const comment = useMemo(
    () => (item && timeline ? pickSourceComment(item, timeline, viewerId) : null),
    [item, timeline, viewerId],
  );
  return { comment, isLoading: enabled && !!issueId && isLoading };
}
