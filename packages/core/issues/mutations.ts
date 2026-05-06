import { useState, useCallback } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import {
  issueKeys,
  ISSUE_PAGE_SIZE,
  type MyIssuesFilter,
} from "./queries";
import {
  addIssueToBuckets,
  findIssueLocation,
  getBucket,
  patchIssueInBuckets,
  removeIssueFromBuckets,
  setBucket,
} from "./cache-helpers";
import { useWorkspaceId } from "../hooks";
import { useRecentIssuesStore } from "./stores";
import type { Issue, IssueReaction, IssueStatus } from "../types";
import type {
  CreateIssueRequest,
  UpdateIssueRequest,
  ListIssuesCache,
} from "../types";
import type { TimelineEntry, IssueSubscriber, Reaction } from "../types";
import {
  mapAllEntries,
  filterAllEntries,
  prependToLatestPage,
  type TimelineCacheData,
} from "./timeline-cache";

// ---------------------------------------------------------------------------
// Shared mutation variable types — used by both mutation hooks and
// useMutationState consumers to keep the type assertion in sync.
// ---------------------------------------------------------------------------

export type ToggleCommentReactionVars = {
  commentId: string;
  emoji: string;
  existing: Reaction | undefined;
};

export type ToggleIssueReactionVars = {
  emoji: string;
  existing: IssueReaction | undefined;
};

// ---------------------------------------------------------------------------
// Per-status pagination
// ---------------------------------------------------------------------------

/**
 * Paginate one status column into the cache. Works for both the workspace
 * issue list and per-scope My Issues lists (pass `myIssues` to target the
 * latter).
 */
export function useLoadMoreByStatus(
  status: IssueStatus,
  myIssues?: { scope: string; filter: MyIssuesFilter },
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [isLoading, setIsLoading] = useState(false);

  const queryKey = myIssues
    ? issueKeys.myList(wsId, myIssues.scope, myIssues.filter)
    : issueKeys.list(wsId);
  const cache = qc.getQueryData<ListIssuesCache>(queryKey);
  const bucket = cache?.byStatus[status];
  const loaded = bucket?.issues.length ?? 0;
  const total = bucket?.total ?? 0;
  const hasMore = loaded < total;

  const loadMore = useCallback(async () => {
    if (isLoading || !hasMore) return;
    setIsLoading(true);
    try {
      const res = await api.listIssues({
        status,
        limit: ISSUE_PAGE_SIZE,
        offset: loaded,
        ...myIssues?.filter,
      });
      qc.setQueryData<ListIssuesCache>(queryKey, (old) => {
        if (!old) return old;
        const prev = getBucket(old, status);
        const existingIds = new Set(prev.issues.map((i) => i.id));
        const appended = res.issues.filter((i) => !existingIds.has(i.id));
        return setBucket(old, status, {
          issues: [...prev.issues, ...appended],
          total: res.total,
        });
      });
    } finally {
      setIsLoading(false);
    }
  }, [qc, queryKey, status, loaded, hasMore, isLoading, myIssues?.filter]);

  return { loadMore, hasMore, isLoading, total };
}

// ---------------------------------------------------------------------------
// Issue CRUD
// ---------------------------------------------------------------------------

export function useCreateIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateIssueRequest) => api.createIssue(data),
    onSuccess: (newIssue) => {
      qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
        old ? addIssueToBuckets(old, newIssue) : old,
      );
      // Surface the just-created issue in cmd+k's Recent list without
      // requiring the user to open it first.
      useRecentIssuesStore.getState().recordVisit(newIssue.id);
      // Invalidate parent's children query so sub-issues list updates immediately
      if (newIssue.parent_issue_id) {
        qc.invalidateQueries({ queryKey: issueKeys.children(wsId, newIssue.parent_issue_id) });
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    },
  });
}

export function useUpdateIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateIssueRequest) =>
      api.updateIssue(id, data),
    onMutate: ({ id, ...data }) => {
      // Fire-and-forget cancelQueries — keeps onMutate synchronous so the
      // cache update happens in the same tick as mutate(). Awaiting would
      // yield to the event loop, letting @dnd-kit reset its visual state
      // before the optimistic update lands.
      qc.cancelQueries({ queryKey: issueKeys.list(wsId) });
      const prevList = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
      const prevDetail = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));

      // Resolve parent_issue_id from the freshest source so we can keep the
      // parent's children cache in sync (used by the parent issue's
      // sub-issues list).
      const parentId =
        prevDetail?.parent_issue_id ??
        (prevList ? findIssueLocation(prevList, id)?.issue.parent_issue_id : null) ??
        null;
      const prevChildren = parentId
        ? qc.getQueryData<Issue[]>(issueKeys.children(wsId, parentId))
        : undefined;

      qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
        old ? patchIssueInBuckets(old, id, data) : old,
      );
      qc.setQueryData<Issue>(issueKeys.detail(wsId, id), (old) =>
        old ? { ...old, ...data } : old,
      );
      if (parentId) {
        qc.setQueryData<Issue[]>(
          issueKeys.children(wsId, parentId),
          (old) =>
            old?.map((c) => (c.id === id ? { ...c, ...data } : c)),
        );
      }
      return { prevList, prevDetail, prevChildren, parentId, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(issueKeys.list(wsId), ctx.prevList);
      if (ctx?.prevDetail)
        qc.setQueryData(issueKeys.detail(wsId, ctx.id), ctx.prevDetail);
      if (ctx?.parentId && ctx.prevChildren !== undefined) {
        qc.setQueryData(
          issueKeys.children(wsId, ctx.parentId),
          ctx.prevChildren,
        );
      }
    },
    onSettled: (_data, _err, vars, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      // Invalidate old parent's children cache
      if (ctx?.parentId) {
        qc.invalidateQueries({
          queryKey: issueKeys.children(wsId, ctx.parentId),
        });
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
      // Invalidate new parent's children cache when parent_issue_id changed
      const newParentId = vars.parent_issue_id;
      if (newParentId && newParentId !== ctx?.parentId) {
        qc.invalidateQueries({
          queryKey: issueKeys.children(wsId, newParentId),
        });
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
    },
  });
}

export function useDeleteIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteIssue(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: issueKeys.list(wsId) });
      const prevList = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
      const deleted = prevList ? findIssueLocation(prevList, id)?.issue : undefined;
      qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) =>
        old ? removeIssueFromBuckets(old, id) : old,
      );
      qc.removeQueries({ queryKey: issueKeys.detail(wsId, id) });
      return { prevList, parentIssueId: deleted?.parent_issue_id };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(issueKeys.list(wsId), ctx.prevList);
    },
    onSettled: (_data, _err, _id, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      if (ctx?.parentIssueId) {
        qc.invalidateQueries({ queryKey: issueKeys.children(wsId, ctx.parentIssueId) });
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
    },
  });
}

export function useBatchUpdateIssues() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      ids,
      updates,
    }: {
      ids: string[];
      updates: UpdateIssueRequest;
    }) => api.batchUpdateIssues(ids, updates),
    onMutate: async ({ ids, updates }) => {
      await qc.cancelQueries({ queryKey: issueKeys.list(wsId) });
      const prevList = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
      qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) => {
        if (!old) return old;
        let next = old;
        for (const id of ids) next = patchIssueInBuckets(next, id, updates);
        return next;
      });
      return { prevList };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(issueKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    },
  });
}

export function useBatchDeleteIssues() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (ids: string[]) => api.batchDeleteIssues(ids),
    onMutate: async (ids) => {
      await qc.cancelQueries({ queryKey: issueKeys.list(wsId) });
      const prevList = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
      const parentIssueIds = new Set<string>();
      if (prevList) {
        for (const id of ids) {
          const loc = findIssueLocation(prevList, id);
          if (loc?.issue.parent_issue_id) parentIssueIds.add(loc.issue.parent_issue_id);
        }
      }
      qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) => {
        if (!old) return old;
        let next = old;
        for (const id of ids) next = removeIssueFromBuckets(next, id);
        return next;
      });
      return { prevList, parentIssueIds };
    },
    onError: (_err, _ids, ctx) => {
      if (ctx?.prevList) qc.setQueryData(issueKeys.list(wsId), ctx.prevList);
    },
    onSettled: (_data, _err, _ids, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      if (ctx?.parentIssueIds && ctx.parentIssueIds.size > 0) {
        for (const parentId of ctx.parentIssueIds) {
          qc.invalidateQueries({ queryKey: issueKeys.children(wsId, parentId) });
        }
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
    },
  });
}

// ---------------------------------------------------------------------------
// Comments / Timeline
// ---------------------------------------------------------------------------

export function useCreateComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      content,
      type,
      parentId,
      attachmentIds,
    }: {
      content: string;
      type?: string;
      parentId?: string;
      attachmentIds?: string[];
    }) => api.createComment(issueId, content, type, parentId, attachmentIds),
    onSuccess: (comment) => {
      // Write into every paginated timeline cache that's currently at-latest
      // (around-mode caches viewing older windows skip silently inside
      // prependToLatestPage). Both the latest cache and any open around-mode
      // window that has been scrolled all the way to the live tail get the
      // optimistic entry; everything else falls back to invalidation.
      const entry: TimelineEntry = {
        type: "comment",
        id: comment.id,
        actor_type: comment.author_type,
        actor_id: comment.author_id,
        content: comment.content,
        parent_id: comment.parent_id,
        comment_type: comment.type,
        reactions: comment.reactions ?? [],
        attachments: comment.attachments ?? [],
        created_at: comment.created_at,
        updated_at: comment.updated_at,
      };
      qc.setQueriesData<TimelineCacheData>(
        { queryKey: ["issues", "timeline", issueId] },
        (old) => prependToLatestPage(old, entry),
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

export function useUpdateComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ commentId, content }: { commentId: string; content: string }) =>
      api.updateComment(commentId, content),
    onMutate: async ({ commentId, content }) => {
      await qc.cancelQueries({ queryKey: ["issues", "timeline", issueId] });
      // Snapshot every open timeline cache (latest + any around windows) so
      // an error rollback restores them all atomically.
      const prevSnapshots = qc.getQueriesData<TimelineCacheData>({
        queryKey: ["issues", "timeline", issueId],
      });
      qc.setQueriesData<TimelineCacheData>(
        { queryKey: ["issues", "timeline", issueId] },
        (old) =>
          mapAllEntries(old, (e) =>
            e.id === commentId ? { ...e, content } : e,
          ),
      );
      return { prevSnapshots };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevSnapshots) {
        for (const [key, prev] of ctx.prevSnapshots) {
          qc.setQueryData(key, prev);
        }
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

export function useDeleteComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (commentId: string) => api.deleteComment(commentId),
    onMutate: async (commentId) => {
      await qc.cancelQueries({ queryKey: ["issues", "timeline", issueId] });
      const prevSnapshots = qc.getQueriesData<TimelineCacheData>({
        queryKey: ["issues", "timeline", issueId],
      });

      // Cascade: collect all child comment IDs across every loaded page.
      const toRemove = new Set<string>([commentId]);
      for (const [, data] of prevSnapshots) {
        if (!data) continue;
        let changed = true;
        while (changed) {
          changed = false;
          for (const page of data.pages) {
            for (const e of page.entries) {
              if (
                e.parent_id &&
                toRemove.has(e.parent_id) &&
                !toRemove.has(e.id)
              ) {
                toRemove.add(e.id);
                changed = true;
              }
            }
          }
        }
      }

      qc.setQueriesData<TimelineCacheData>(
        { queryKey: ["issues", "timeline", issueId] },
        (old) => filterAllEntries(old, (e) => toRemove.has(e.id)),
      );
      return { prevSnapshots };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevSnapshots) {
        for (const [key, prev] of ctx.prevSnapshots) {
          qc.setQueryData(key, prev);
        }
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

export function useToggleCommentReaction(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ["toggleCommentReaction", issueId] as const,
    mutationFn: async ({
      commentId,
      emoji,
      existing,
    }: ToggleCommentReactionVars) => {
      if (existing) {
        await api.removeReaction(commentId, emoji);
        return null;
      }
      return api.addReaction(commentId, emoji);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Issue-level Reactions
// ---------------------------------------------------------------------------

export function useToggleIssueReaction(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ["toggleIssueReaction", issueId] as const,
    mutationFn: async ({
      emoji,
      existing,
    }: ToggleIssueReactionVars) => {
      if (existing) {
        await api.removeIssueReaction(issueId, emoji);
        return null;
      }
      return api.addIssueReaction(issueId, emoji);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.reactions(issueId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Issue Subscribers
// ---------------------------------------------------------------------------

export function useToggleIssueSubscriber(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      userId,
      userType,
      subscribed,
    }: {
      userId: string;
      userType: "member" | "agent";
      subscribed: boolean;
    }) => {
      if (subscribed) {
        await api.unsubscribeFromIssue(issueId, userId, userType);
      } else {
        await api.subscribeToIssue(issueId, userId, userType);
      }
    },
    onMutate: async ({ userId, userType, subscribed }) => {
      await qc.cancelQueries({ queryKey: issueKeys.subscribers(issueId) });
      const prev = qc.getQueryData<IssueSubscriber[]>(
        issueKeys.subscribers(issueId),
      );

      if (subscribed) {
        qc.setQueryData<IssueSubscriber[]>(
          issueKeys.subscribers(issueId),
          (old) =>
            old?.filter(
              (s) => !(s.user_id === userId && s.user_type === userType),
            ),
        );
      } else {
        const temp: IssueSubscriber = {
          issue_id: issueId,
          user_type: userType,
          user_id: userId,
          reason: "manual",
          created_at: new Date().toISOString(),
        };
        qc.setQueryData<IssueSubscriber[]>(
          issueKeys.subscribers(issueId),
          (old) => {
            if (
              old?.some(
                (s) => s.user_id === userId && s.user_type === userType,
              )
            )
              return old;
            return [...(old ?? []), temp];
          },
        );
      }
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev)
        qc.setQueryData(issueKeys.subscribers(issueId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.subscribers(issueId) });
    },
  });
}
