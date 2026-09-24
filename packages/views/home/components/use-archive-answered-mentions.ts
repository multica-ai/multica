"use client";

import { useEffect, useRef } from "react";
import { useQueries } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import type { NeedsMeItem } from "@multica/core/home";
import { useArchiveInbox } from "@multica/core/inbox/mutations";
import { issueTimelineOptions } from "@multica/core/issues/queries";

// Bounds the per-open timeline reads; older mentions settle on a later visit.
const MAX_CHECKED_MENTIONS = 10;

/**
 * A mention answered on the issue page never passes through the queue, so its
 * inbox row would sit in "needs you" forever. When Home or the queue opens,
 * check each queued mention's timeline: if the viewer has commented after the
 * mention arrived, archive it — the reply is the action the mention asked for.
 */
export function useArchiveAnsweredMentions(items: readonly NeedsMeItem[]) {
  const viewerId = useAuthStore((s) => s.user?.id);
  const archive = useArchiveInbox();
  const archiveMutate = archive.mutate;
  const attempted = useRef(new Set<string>());

  const mentions = items
    .filter((item) => item.kind === "mentioned" && item.issueId && item.inbox)
    .slice(0, MAX_CHECKED_MENTIONS);
  const timelines = useQueries({
    queries: mentions.map((item) => ({ ...issueTimelineOptions(item.issueId!) })),
  });

  useEffect(() => {
    if (!viewerId) return;
    mentions.forEach((item, index) => {
      const timeline = timelines[index]?.data;
      const mention = item.inbox;
      if (!timeline || !mention || attempted.current.has(item.key)) return;
      const mentionedAt = new Date(mention.created_at).getTime();
      const answered = timeline.some(
        (entry) =>
          entry.type === "comment" &&
          entry.actor_type === "member" &&
          entry.actor_id === viewerId &&
          new Date(entry.created_at).getTime() > mentionedAt,
      );
      if (!answered) return;
      attempted.current.add(item.key);
      // Archiving one row archives every row for the same issue.
      archiveMutate(mention.id);
    });
  });
}
