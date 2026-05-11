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
      useRecentIssuesStore.getState().recordVisit(wsId, newIssue.id);
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
      // sub-issues list). Falls back to scanning loaded children caches —
      // when the user navigates straight to a parent's detail page, the
      // child may live only there, not in detail/list.
      let parentId: string | null =
        prevDetail?.parent_issue_id ??
        (prevList ? findIssueLocation(prevList, id)?.issue.parent_issue_id : null) ??
        null;
      if (!parentId) {
        const childrenCaches = qc.getQueriesData<Issue[]>({
          queryKey: [...issueKeys.all(wsId), "children"],
        });
        for (const [key, data] of childrenCaches) {
          if (!data?.some((c) => c.id === id)) continue;
          const candidate = key[key.length - 1];
          if (typeof candidate === "string") {
            parentId = candidate;
            break;
          }
        }
      }
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

      // Mirror the optimistic patch into any loaded children cache so
      // sub-issue rows on a parent's detail page reflect the change too.
      const idSet = new Set(ids);
      const childrenCaches = qc.getQueriesData<Issue[]>({
        queryKey: [...issueKeys.all(wsId), "children"],
      });
      const prevChildren = new Map<string, Issue[] | undefined>();
      const affectedParentIds = new Set<string>();
      for (const [key, data] of childrenCaches) {
        if (!data?.some((c) => idSet.has(c.id))) continue;
        const parentId = key[key.length - 1];
        if (typeof parentId !== "string") continue;
        affectedParentIds.add(parentId);
        prevChildren.set(parentId, data);
        qc.setQueryData<Issue[]>(issueKeys.children(wsId, parentId), (old) =>
          old?.map((c) => (idSet.has(c.id) ? { ...c, ...updates } : c)),
        );
      }

      return { prevList, prevChildren, affectedParentIds };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(issueKeys.list(wsId), ctx.prevList);
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSettled: (_data, _err, _vars, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      if (ctx?.affectedParentIds && ctx.affectedParentIds.size > 0) {
        for (const parentId of ctx.affectedParentIds) {
          qc.invalidateQueries({
            queryKey: issueKeys.children(wsId, parentId),
          });
        }
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
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
      // Children cache may be the only place sub-issues live when the user
      // operates from a parent's detail page. Collect affected parents and
      // optimistically filter the deleted ids out of each children cache so
      // the row disappears immediately, mirroring the list-cache behaviour.
      const idSet = new Set(ids);
      const childrenCaches = qc.getQueriesData<Issue[]>({
        queryKey: [...issueKeys.all(wsId), "children"],
      });
      const prevChildren = new Map<string, Issue[] | undefined>();
      for (const [key, data] of childrenCaches) {
        if (!data?.some((c) => idSet.has(c.id))) continue;
        const parentId = key[key.length - 1];
        if (typeof parentId !== "string") continue;
        parentIssueIds.add(parentId);
        prevChildren.set(parentId, data);
        qc.setQueryData<Issue[]>(issueKeys.children(wsId, parentId), (old) =>
          old?.filter((c) => !idSet.has(c.id)),
        );
      }
      qc.setQueryData<ListIssuesCache>(issueKeys.list(wsId), (old) => {
        if (!old) return old;
        let next = old;
        for (const id of ids) next = removeIssueFromBuckets(next, id);
        return next;
      });
      return { prevList, prevChildren, parentIssueIds };
    },
    onError: (_err, _ids, ctx) => {
      if (ctx?.prevList) qc.setQueryData(issueKeys.list(wsId), ctx.prevList);
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
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

type TimelineCache = TimelineEntry[];

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
      // Dedupe by id: the `comment:created` WS event may have already added
      // this entry from the broadcast path before this onSuccess fires. Skip
      // the append if the entry is already in the cache.
      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) => {
        if (!old) return [entry];
        if (old.some((e) => e.id === entry.id)) return old;
        return [...old, entry];
      });
    },
    // No onSettled invalidate. The `comment:created` WS broadcast keeps
    // the timeline cache fresh after a successful create, and reconnect
    // recovery in useIssueTimeline already invalidates if the connection
    // dropped. Re-fetching on every submit replaces every entry's
    // reference, which forces every memoized CommentCard subtree to
    // re-render (visible as a flash across sibling threads during AI
    // streaming).
  });
}

export function useUpdateComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ commentId, content }: { commentId: string; content: string }) =>
      api.updateComment(commentId, content),
    onMutate: async ({ commentId, content }) => {
      await qc.cancelQueries({ queryKey: issueKeys.timeline(issueId) });
      const prev = qc.getQueryData<TimelineCache>(issueKeys.timeline(issueId));
      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) =>
        old?.map((e) => (e.id === commentId ? { ...e, content } : e)),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(issueKeys.timeline(issueId), ctx.prev);
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
      await qc.cancelQueries({ queryKey: issueKeys.timeline(issueId) });
      const prev = qc.getQueryData<TimelineCache>(issueKeys.timeline(issueId));

      // Cascade: collect all descendants of the deleted comment.
      const toRemove = new Set<string>([commentId]);
      if (prev) {
        let changed = true;
        while (changed) {
          changed = false;
          for (const e of prev) {
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

      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) =>
        old?.filter((e) => !toRemove.has(e.id)),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(issueKeys.timeline(issueId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

export function useResolveComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ commentId, resolved }: { commentId: string; resolved: boolean }) =>
      resolved ? api.resolveComment(commentId) : api.unresolveComment(commentId),
    onMutate: async ({ commentId, resolved }) => {
      await qc.cancelQueries({ queryKey: issueKeys.timeline(issueId) });
      const prev = qc.getQueryData<TimelineCache>(issueKeys.timeline(issueId));
      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) =>
        old?.map((e) =>
          e.id === commentId
            ? {
                ...e,
                resolved_at: resolved ? new Date().toISOString() : null,
                resolved_by_type: resolved ? e.resolved_by_type ?? null : null,
                resolved_by_id: resolved ? e.resolved_by_id ?? null : null,
              }
            : e,
        ),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(issueKeys.timeline(issueId), ctx.prev);
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
