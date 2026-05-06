"use client";

import { useEffect, useRef, useState, useCallback, useMemo } from "react";
import {
  useInfiniteQuery,
  useQueryClient,
  useMutationState,
} from "@tanstack/react-query";
import type {
  Comment,
  TimelineEntry,
  Reaction,
} from "@multica/core/types";
import type {
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
} from "@multica/core/types";
import {
  issueTimelineInfiniteOptions,
  issueKeys,
} from "@multica/core/issues/queries";
import {
  mapAllEntries,
  filterAllEntries,
  prependToLatestPage,
  type TimelineCacheData,
} from "@multica/core/issues/timeline-cache";
import {
  useCreateComment,
  useUpdateComment,
  useDeleteComment,
  useToggleCommentReaction,
  type ToggleCommentReactionVars,
} from "@multica/core/issues/mutations";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import { toast } from "sonner";
import { useT } from "../../i18n";

type TLData = TimelineCacheData;

// Dev-only invariant: fire loudly if any WS/mutation path hands a non-array-
// shaped cache to a setQueryData updater. Production behavior is already
// tolerant (shape guards in timeline-cache.ts + the flattening memo below),
// so this exists purely to surface the upstream polluter in dev/test — the
// thing that got us upstream#2143 / #2147 in the first place. NODE_ENV is
// checked at call time so tests can flip to "production" via vi.stubEnv.
function assertTimelineShape(label: string, data: TLData | undefined): void {
  if (process.env.NODE_ENV === "production") return;
  if (data === undefined) return;
  if (!data || typeof data !== "object") {
    throw new Error(
      `[timeline-cache/${label}] invariant: cache root has type ${typeof data}`,
    );
  }
  if (!Array.isArray(data.pages)) {
    throw new Error(
      `[timeline-cache/${label}] invariant: data.pages is not an array`,
    );
  }
  for (let i = 0; i < data.pages.length; i++) {
    const page = data.pages[i];
    if (!page || !Array.isArray(page.entries)) {
      throw new Error(
        `[timeline-cache/${label}] invariant: data.pages[${i}].entries is not an array`,
      );
    }
  }
}

function commentToTimelineEntry(c: Comment): TimelineEntry {
  return {
    type: "comment",
    id: c.id,
    actor_type: c.author_type,
    actor_id: c.author_id,
    content: c.content,
    parent_id: c.parent_id,
    created_at: c.created_at,
    updated_at: c.updated_at,
    comment_type: c.type,
    reactions: c.reactions ?? [],
    attachments: c.attachments ?? [],
  };
}

export interface UseIssueTimelineOptions {
  /** Anchor the initial fetch on this entry id (Inbox jump path). When set,
   *  the first page is centered on the target instead of the latest 50. */
  around?: string | null;
}

export function useIssueTimeline(
  issueId: string,
  userId?: string,
  options: UseIssueTimelineOptions = {},
) {
  const { t } = useT("issues");
  const qc = useQueryClient();

  // Internal anchor state. Starts as the caller's around prop; jumpToLatest
  // clears it. A new around prop (e.g. user clicks a different inbox item)
  // resets it via the effect below.
  const [around, setAround] = useState<string | null>(options.around ?? null);
  useEffect(() => {
    if (options.around) setAround(options.around);
  }, [options.around]);

  const query = useInfiniteQuery(issueTimelineInfiniteOptions(issueId, around));
  const {
    data,
    isLoading: loading,
    fetchNextPage,
    fetchPreviousPage,
    hasNextPage,
    hasPreviousPage,
    isFetchingNextPage,
    isFetchingPreviousPage,
  } = query;

  // isAtLatest is the cache-invariant we use to decide where WS-delivered
  // entries belong. It's true when the FIRST loaded page reports no newer
  // entries on the server — i.e. the user is looking at the live tail.
  //
  // Same shape-guard as the timeline memo below: a malformed `data.pages`
  // (not an array) previously crashed `data.pages[0]` and white-screened the
  // whole issue route (upstream#2143 / #2147).
  const isAtLatest = Array.isArray(data?.pages) && data!.pages[0]?.has_more_after === false;

  const [submitting, setSubmitting] = useState(false);
  const [newEntriesBelowCount, setNewEntriesBelowCount] = useState(0);

  // Flatten pages → ASC array for the legacy UI consumer. pages are DESC
  // newest-first; the consumer (issue-detail.tsx) renders chronologically
  // (oldest at top). Concat → DESC; reverse once at the end → ASC.
  //
  // Defensive on pages / entries shape: consumers like `issue-detail.tsx`
  // call `timeline.filter(...)` directly. A malformed cache entry (stale
  // shape from a persisted older build, gzip/proxy mishap, hand-written
  // setQueryData in a future caller) would otherwise white-screen the whole
  // tab (see upstream#2143 / #2147: `timeline.filter is not a function`).
  // Coerce to `[]` on any unexpected shape so the UI degrades to empty
  // instead of crashing the route.
  const timeline = useMemo<TimelineEntry[]>(() => {
    if (!data || !Array.isArray(data.pages)) return [];
    const flat: TimelineEntry[] = [];
    for (const page of data.pages) {
      if (!page || !Array.isArray(page.entries)) continue;
      for (const entry of page.entries) flat.push(entry);
    }
    return flat.reverse();
  }, [data]);

  // Stable mutation handles. TanStack v5 returns a fresh result wrapper from
  // useMutation per render, but the inner mutateAsync / mutate functions are
  // stable. Pull just those so the useCallback identities downstream don't
  // flip on every parent re-render — listing the whole mutation object would
  // defeat React.memo on CommentCard.
  const { mutateAsync: createComment } = useCreateComment(issueId);
  const { mutateAsync: updateComment } = useUpdateComment(issueId);
  const { mutateAsync: deleteCommentAsync } = useDeleteComment(issueId);
  const { mutate: toggleCommentReaction } = useToggleCommentReaction(issueId);

  // Reconnect recovery: drop the cache so the next render refetches the
  // latest page from scratch. We don't try to reconcile diffs over a
  // possibly-long disconnect — easier to start fresh.
  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId, around) });
    }, [qc, issueId, around]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "comment:created",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentCreatedPayload;
        if (comment.issue_id !== issueId) return;
        if (isAtLatest) {
          qc.setQueryData<TLData>(issueKeys.timeline(issueId, around), (old: TLData | undefined) => {
            assertTimelineShape("comment:created", old);
            return prependToLatestPage(old, commentToTimelineEntry(comment));
          });
        } else {
          // Reading older history — don't yank scroll position. Surface a
          // counter so the UI can offer "jump to latest (N new)".
          setNewEntriesBelowCount((c) => c + 1);
        }
      },
      [qc, issueId, around, isAtLatest],
    ),
  );

  useWSEvent(
    "comment:updated",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUpdatedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLData>(issueKeys.timeline(issueId, around), (old: TLData | undefined) => {
          assertTimelineShape("comment:updated", old);
          return mapAllEntries(old, (e) =>
            e.id === comment.id ? commentToTimelineEntry(comment) : e,
          );
        });
      },
      [qc, issueId, around],
    ),
  );

  useWSEvent(
    "comment:deleted",
    useCallback(
      (payload: unknown) => {
        const { comment_id, issue_id } = payload as CommentDeletedPayload;
        if (issue_id !== issueId) return;
        // Cascade through replies. Walk pages collectively; a reply may live
        // on a different page than its parent.
        qc.setQueryData<TLData>(issueKeys.timeline(issueId, around), (old: TLData | undefined) => {
          assertTimelineShape("comment:deleted", old);
          if (!old || !Array.isArray(old.pages)) return old;
          const idsToRemove = new Set<string>([comment_id]);
          let changed = true;
          while (changed) {
            changed = false;
            for (const page of old.pages) {
              if (!page || !Array.isArray(page.entries)) continue;
              for (const e of page.entries) {
                if (
                  e.parent_id &&
                  idsToRemove.has(e.parent_id) &&
                  !idsToRemove.has(e.id)
                ) {
                  idsToRemove.add(e.id);
                  changed = true;
                }
              }
            }
          }
          return filterAllEntries(old, (e) => idsToRemove.has(e.id));
        });
      },
      [qc, issueId, around],
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
        if (isAtLatest) {
          qc.setQueryData<TLData>(issueKeys.timeline(issueId, around), (old: TLData | undefined) => {
            assertTimelineShape("activity:created", old);
            return prependToLatestPage(old, entry);
          });
        } else {
          setNewEntriesBelowCount((c) => c + 1);
        }
      },
      [qc, issueId, around, isAtLatest],
    ),
  );

  useWSEvent(
    "reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id } = payload as ReactionAddedPayload;
        if (issue_id !== issueId) return;
        qc.setQueryData<TLData>(issueKeys.timeline(issueId, around), (old: TLData | undefined) => {
          assertTimelineShape("reaction:added", old);
          return mapAllEntries(old, (e) => {
            if (e.id !== reaction.comment_id) return e;
            const existing = e.reactions ?? [];
            if (existing.some((r) => r.id === reaction.id)) return e;
            return { ...e, reactions: [...existing, reaction] };
          });
        });
      },
      [qc, issueId, around],
    ),
  );

  useWSEvent(
    "reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as ReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        qc.setQueryData<TLData>(issueKeys.timeline(issueId, around), (old: TLData | undefined) => {
          assertTimelineShape("reaction:removed", old);
          return mapAllEntries(old, (e) => {
            if (e.id !== p.comment_id) return e;
            return {
              ...e,
              reactions: (e.reactions ?? []).filter(
                (r) =>
                  !(
                    r.emoji === p.emoji &&
                    r.actor_type === p.actor_type &&
                    r.actor_id === p.actor_id
                  ),
              ),
            };
          });
        });
      },
      [qc, issueId, around],
    ),
  );

  // --- Page navigation ---

  const fetchOlder = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage) fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const fetchNewer = useCallback(() => {
    if (hasPreviousPage && !isFetchingPreviousPage) fetchPreviousPage();
  }, [hasPreviousPage, isFetchingPreviousPage, fetchPreviousPage]);

  const jumpToLatest = useCallback(() => {
    // Drop any anchor + prefetched windows. The latest cache (around=null)
    // may be cold; an invalidate forces a fresh fetch on next render.
    setAround(null);
    setNewEntriesBelowCount(0);
    qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId, null) });
  }, [qc, issueId]);

  // --- Mutation functions ---

  const submitComment = useCallback(
    async (content: string, attachmentIds?: string[]) => {
      if (!content.trim() || submitting || !userId) return;
      setSubmitting(true);
      try {
        await createComment({ content, attachmentIds });
      } catch {
        toast.error(t(($) => $.comment.send_failed));
      } finally {
        setSubmitting(false);
      }
    },
    [userId, submitting, createComment, t],
  );

  const submitReply = useCallback(
    async (parentId: string, content: string, attachmentIds?: string[]) => {
      if (!content.trim() || !userId) return;
      try {
        await createComment({
          content,
          type: "comment",
          parentId,
          attachmentIds,
        });
      } catch {
        toast.error(t(($) => $.comment.send_reply_failed));
      }
    },
    [userId, createComment, t],
  );

  const editComment = useCallback(
    async (commentId: string, content: string) => {
      try {
        await updateComment({ commentId, content });
      } catch {
        toast.error(t(($) => $.comment.update_failed));
      }
    },
    [updateComment, t],
  );

  const deleteComment = useCallback(
    async (commentId: string) => {
      try {
        await deleteCommentAsync(commentId);
      } catch {
        toast.error(t(($) => $.comment.delete_failed));
      }
    },
    [deleteCommentAsync, t],
  );

  // --- Optimistic UI for comment reactions ---
  // Derive at render time from pending mutation variables instead of writing
  // temp data into the cache (which would race with WS events).

  const pendingReactionVars = useMutationState({
    filters: {
      mutationKey: ["toggleCommentReaction", issueId],
      status: "pending",
    },
    select: (m) =>
      m.state.variables as ToggleCommentReactionVars | undefined,
  });

  const optimisticTimeline = useMemo(() => {
    if (pendingReactionVars.length === 0) return timeline;

    return timeline.map((entry) => {
      const pendingForEntry = pendingReactionVars.filter(
        (v) => v && v.commentId === entry.id,
      );
      if (pendingForEntry.length === 0) return entry;

      let reactions = entry.reactions ?? [];
      for (const vars of pendingForEntry) {
        if (!vars) continue;
        if (vars.existing) {
          reactions = reactions.filter((r) => r.id !== vars.existing!.id);
        } else {
          const alreadyExists = reactions.some(
            (r) =>
              r.emoji === vars.emoji &&
              r.actor_type === "member" &&
              r.actor_id === userId,
          );
          if (!alreadyExists) {
            reactions = [
              ...reactions,
              {
                id: `optimistic-${vars.emoji}`,
                comment_id: vars.commentId,
                actor_type: "member",
                actor_id: userId ?? "",
                emoji: vars.emoji,
                created_at: "",
              },
            ];
          }
        }
      }
      return { ...entry, reactions };
    });
  }, [timeline, pendingReactionVars, userId]);

  // toggleReaction reads from a ref so its identity does not change with
  // every WS event. Without this every memoized CommentCard down-tree would
  // re-render on each timeline mutation, defeating the React.memo cost
  // savings on long timelines (#1968).
  const timelineRef = useRef(timeline);
  useEffect(() => {
    timelineRef.current = timeline;
  }, [timeline]);

  const toggleReaction = useCallback(
    async (commentId: string, emoji: string) => {
      if (!userId) return;
      const entry = timelineRef.current.find((e) => e.id === commentId);
      const existing: Reaction | undefined = (entry?.reactions ?? []).find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggleCommentReaction({ commentId, emoji, existing });
    },
    [userId, toggleCommentReaction],
  );

  // Around-mode anchor index (target_index from server, applied within the
  // first page). Translated to a flat-array index: the array is reversed
  // (DESC pages → ASC flat), so the offset within page[0] becomes
  // (totalEntries - 1) - target_index.
  const targetFlatIndex = useMemo(() => {
    if (!data || !Array.isArray(data.pages) || data.pages.length === 0) return null;
    const first = data.pages[0];
    if (!first || !Array.isArray(first.entries) || first.target_index == null) return null;
    let total = 0;
    for (const p of data.pages) {
      if (p && Array.isArray(p.entries)) total += p.entries.length;
    }
    return total - 1 - first.target_index;
  }, [data]);

  return {
    timeline: optimisticTimeline,
    loading,
    submitting,
    submitComment,
    submitReply,
    editComment,
    deleteComment,
    toggleReaction,
    // Pagination controls (new)
    hasMoreOlder: hasNextPage,
    hasMoreNewer: hasPreviousPage,
    isFetchingOlder: isFetchingNextPage,
    isFetchingNewer: isFetchingPreviousPage,
    fetchOlder,
    fetchNewer,
    jumpToLatest,
    isAtLatest: isAtLatest === true,
    newEntriesBelowCount,
    targetFlatIndex,
  };
}
