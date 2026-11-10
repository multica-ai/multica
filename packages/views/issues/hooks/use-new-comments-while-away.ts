import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { TimelineEntry } from "@multica/core/types";
import { isDeletedComment } from "@multica/core/issues/comment-deletion";

/**
 * Comments that arrived while the reader was away from the latest entry
 * (MUL-8951), oldest first, so "Jump to latest" can say how many landed and
 * take the reader to the first of them.
 *
 * The comments present once the timeline has loaded are the baseline; only
 * later arrivals count, and only while `away`. The reader's own comments never
 * count — posting one already scrolls to it. The list empties as soon as the
 * reader is no longer away, and starts over when `resetKey` (the issue)
 * changes, since the issue page is reused across issues.
 */
export function useNewCommentsWhileAway(
  timeline: readonly TimelineEntry[],
  {
    ready,
    away,
    userId,
    resetKey,
  }: { ready: boolean; away: boolean; userId: string | undefined; resetKey: string },
): { ids: readonly string[]; clear: () => void } {
  const [ids, setIds] = useState<string[]>([]);
  const seenRef = useRef<Set<string> | null>(null);
  const keyRef = useRef(resetKey);
  // Read inside the timeline effect: an arrival grows the page before the
  // away flag is re-measured, so this is still the state the reader was in.
  const awayRef = useRef(away);
  awayRef.current = away;

  useEffect(() => {
    if (keyRef.current !== resetKey) {
      keyRef.current = resetKey;
      seenRef.current = null;
      setIds((prev) => (prev.length === 0 ? prev : []));
    }
    if (!ready) return;
    const comments = timeline.filter((entry) => entry.type === "comment");
    if (seenRef.current === null) {
      seenRef.current = new Set(comments.map((entry) => entry.id));
      return;
    }
    const seen = seenRef.current;
    const arrived: string[] = [];
    for (const entry of comments) {
      if (seen.has(entry.id)) continue;
      seen.add(entry.id);
      if (!awayRef.current || isDeletedComment(entry)) continue;
      if (entry.actor_type === "member" && entry.actor_id === userId) continue;
      arrived.push(entry.id);
    }
    if (arrived.length > 0) setIds((prev) => [...prev, ...arrived]);
  }, [timeline, ready, resetKey, userId]);

  useEffect(() => {
    if (!away) setIds((prev) => (prev.length === 0 ? prev : []));
  }, [away]);

  const clear = useCallback(() => setIds((prev) => (prev.length === 0 ? prev : [])), []);

  // A new comment deleted before the reader gets to it no longer counts.
  const live = useMemo(() => {
    if (ids.length === 0) return ids;
    const present = new Set(
      timeline.filter((entry) => entry.type === "comment" && !isDeletedComment(entry)).map((entry) => entry.id),
    );
    return ids.filter((id) => present.has(id));
  }, [ids, timeline]);

  return { ids: live, clear };
}
