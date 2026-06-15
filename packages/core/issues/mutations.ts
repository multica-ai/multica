import { useState, useCallback } from "react";
import { useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { api } from "../api";
import {
  issueKeys,
  ISSUE_PAGE_SIZE,
  type AssigneeGroupedIssuesFilter,
  type IssueSortParam,
  type MyIssuesFilter,
} from "./queries";
import { projectKeys } from "../projects/queries";
import {
  addIssueToBuckets,
  findIssueLocation,
  getBucket,
  patchIssueInBuckets,
  setBucket,
} from "./cache-helpers";
import {
  cleanupDeletedIssueCaches,
  collectDeletedIssueCacheMetadata,
  invalidateDeletedIssueDependentCaches,
  invalidateDeletedIssueParentCaches,
  invalidateIssueScopedCaches,
  pruneDeletedIssueFromListCaches,
  pruneDeletedIssueFromParentChildrenCaches,
} from "./delete-cache";
import { useWorkspaceId } from "../hooks";
import { useRecentContextStore } from "../chat/recent-context-store";
import { useRecentIssuesStore } from "./stores";
import type { GroupedIssuesResponse, Issue, IssueAssigneeGroup, IssueReaction, IssueStatus } from "../types";
import type {
  CreateIssueRequest,
  UpdateIssueRequest,
  ListIssuesCache,
} from "../types";
import type { TimelineEntry, IssueSubscriber, Reaction } from "../types";
import { sortTimelineEntriesAsc } from "./timeline-sort";

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
 *
 * `sort` must match the sort the consuming `useQuery` was called with —
 * the query key embeds it (see `listSorted` / `myListSorted`), so a load-more
 * with the wrong sort would patch a stale cache entry that nobody is
 * subscribed to. It is also threaded into the API request so the appended
 * page lines up with the server-side ordering of the existing items.
 */
export function useLoadMoreByStatus(
  status: IssueStatus,
  myIssues?: { scope: string; filter: MyIssuesFilter },
  sort?: IssueSortParam,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [isLoading, setIsLoading] = useState(false);

  const activeKey = myIssues
    ? issueKeys.myListSorted(wsId, myIssues.scope, myIssues.filter, sort)
    : issueKeys.listSorted(wsId, sort);
  const cache = qc.getQueryData<ListIssuesCache>(activeKey);
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
        ...sort,
        ...myIssues?.filter,
      });
      qc.setQueryData<ListIssuesCache>(activeKey, (old) => {
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
  }, [qc, activeKey, status, loaded, hasMore, isLoading, myIssues?.filter, sort]);

  return { loadMore, hasMore, isLoading, total };
}

/**
 * Paginate one assignee-grouped board column into the cache. `queryKey`
 * already pins the active cache entry (it's the same object the consuming
 * `useQuery` registered), so the cache lookup and `setQueryData` target the
 * right row. `sort` is threaded into the API request so the appended page
 * lines up with the server-side ordering of the existing items.
 */
export function useLoadMoreByAssigneeGroup(
  group: Pick<IssueAssigneeGroup, "id" | "assignee_type" | "assignee_id">,
  queryKey: QueryKey,
  filter: AssigneeGroupedIssuesFilter,
  sort?: IssueSortParam,
) {
  const qc = useQueryClient();
  const [isLoading, setIsLoading] = useState(false);

  const cache = qc.getQueryData<GroupedIssuesResponse>(queryKey);
  const cachedGroup = cache?.groups.find((g) => g.id === group.id);
  const loaded = cachedGroup?.issues.length ?? 0;
  const total = cachedGroup?.total ?? 0;
  const hasMore = loaded < total;

  const loadMore = useCallback(async () => {
    if (isLoading || !hasMore) return;
    setIsLoading(true);
    try {
      const res = await api.listGroupedIssues({
        group_by: "assignee",
        limit: ISSUE_PAGE_SIZE,
        offset: loaded,
        ...sort,
        ...filter,
        group_assignee_type: group.assignee_type ?? "none",
        group_assignee_id: group.assignee_id ?? undefined,
      });
      const nextGroup = res.groups[0];
      if (!nextGroup) return;

      qc.setQueryData<GroupedIssuesResponse>(queryKey, (old) => {
        if (!old) return old;
        return {
          groups: old.groups.map((existing) => {
            if (existing.id !== nextGroup.id) return existing;
            const existingIds = new Set(existing.issues.map((issue) => issue.id));
            const appended = nextGroup.issues.filter((issue) => !existingIds.has(issue.id));
            return {
              ...existing,
              issues: [...existing.issues, ...appended],
              total: nextGroup.total,
            };
          }),
        };
      });
    } finally {
      setIsLoading(false);
    }
  }, [filter, group.assignee_id, group.assignee_type, hasMore, isLoading, loaded, qc, queryKey, sort]);

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
      for (const [key, data] of qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) })) {
        if (data) qc.setQueryData<ListIssuesCache>(key, addIssueToBuckets(data, newIssue));
      }
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
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
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
      const prevLists = qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) });
      const firstListData = prevLists[0]?.[1];
      const prevDetail = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));

      // Resolve parent_issue_id from the freshest source so we can keep the
      // parent's children cache in sync (used by the parent issue's
      // sub-issues list). Falls back to scanning loaded children caches —
      // when the user navigates straight to a parent's detail page, the
      // child may live only there, not in detail/list.
      let parentId: string | null =
        prevDetail?.parent_issue_id ??
        (firstListData ? findIssueLocation(firstListData, id)?.issue.parent_issue_id : null) ??
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

      for (const [key, cached] of prevLists) {
        if (cached) qc.setQueryData<ListIssuesCache>(key, patchIssueInBuckets(cached, id, data));
      }
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
      return { prevLists, prevDetail, prevChildren, parentId, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
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
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      if (
        vars.status !== undefined ||
        Object.prototype.hasOwnProperty.call(vars, "project_id")
      ) {
        qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      }
      // Refresh the issue's attachments cache when the description editor
      // bound new uploads — the description editor reads `issueAttachments`
      // to resolve text-preview Eye gates, and unlike other mutations this
      // payload mutates the attachment join table.
      if (vars.attachment_ids?.length) {
        qc.invalidateQueries({ queryKey: issueKeys.attachments(vars.id) });
      }
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
      // Invalidate the batched-children cache only when the parent link
      // actually changed. The WS path (ws-updaters.ts) invalidates
      // unconditionally because it doesn't know what the server change
      // touched; here onMutate already patched issueKeys.children(parent)
      // optimistically, so we only need to flush when the parent relation
      // itself moved.
      if (ctx?.parentId || newParentId) {
        qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
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
      await Promise.all([
        qc.cancelQueries({ queryKey: issueKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: issueKeys.myAll(wsId) }),
      ]);
      const metadata = collectDeletedIssueCacheMetadata(qc, wsId, id);
      await Promise.all(
        metadata.parentIssueIds.map((parentId) =>
          qc.cancelQueries({ queryKey: issueKeys.children(wsId, parentId) }),
        ),
      );
      const prevLists = qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) });
      const prevMyLists = qc.getQueriesData<ListIssuesCache>({
        queryKey: issueKeys.myAll(wsId),
      });
      const prevDetail = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));
      const prevChildren = new Map<string, Issue[] | undefined>();
      for (const parentId of metadata.parentIssueIds) {
        prevChildren.set(
          parentId,
          qc.getQueryData<Issue[]>(issueKeys.children(wsId, parentId)),
        );
      }

      pruneDeletedIssueFromListCaches(qc, wsId, id);
      pruneDeletedIssueFromParentChildrenCaches(qc, wsId, id, metadata);
      qc.removeQueries({ queryKey: issueKeys.detail(wsId, id) });
      return { id, metadata, prevLists, prevMyLists, prevDetail, prevChildren };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevDetail) {
        qc.setQueryData(issueKeys.detail(wsId, ctx.id), ctx.prevDetail);
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSuccess: (_data, id, ctx) => {
      useRecentContextStore.getState().forgetContext(wsId, { type: "issue", id });
      cleanupDeletedIssueCaches(qc, wsId, id, ctx?.metadata);
    },
    onSettled: (_data, _err, _id, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      if (ctx?.metadata) invalidateDeletedIssueParentCaches(qc, wsId, ctx.metadata);
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
      const prevLists = qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) });
      for (const [key, cached] of prevLists) {
        if (!cached) continue;
        let next = cached;
        for (const id of ids) next = patchIssueInBuckets(next, id, updates);
        qc.setQueryData<ListIssuesCache>(key, next);
      }

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

      return { prevLists, prevChildren, affectedParentIds };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSettled: (_data, _err, _vars, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      if (
        _vars.updates.status !== undefined ||
        Object.prototype.hasOwnProperty.call(_vars.updates, "project_id")
      ) {
        qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      }
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
      await Promise.all([
        qc.cancelQueries({ queryKey: issueKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: issueKeys.myAll(wsId) }),
      ]);
      const metadataById = new Map(
        ids.map((id) => [
          id,
          collectDeletedIssueCacheMetadata(qc, wsId, id),
        ]),
      );
      const parentIssueIds = new Set<string>();
      for (const metadata of metadataById.values()) {
        for (const parentId of metadata.parentIssueIds) {
          parentIssueIds.add(parentId);
        }
      }
      await Promise.all(
        Array.from(parentIssueIds).map((parentId) =>
          qc.cancelQueries({ queryKey: issueKeys.children(wsId, parentId) }),
        ),
      );
      const prevLists = qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) });
      const prevMyLists = qc.getQueriesData<ListIssuesCache>({
        queryKey: issueKeys.myAll(wsId),
      });
      const prevChildren = new Map<string, Issue[] | undefined>();
      for (const parentId of parentIssueIds) {
        prevChildren.set(
          parentId,
          qc.getQueryData<Issue[]>(issueKeys.children(wsId, parentId)),
        );
      }

      for (const id of ids) {
        const metadata = metadataById.get(id);
        pruneDeletedIssueFromListCaches(qc, wsId, id);
        if (metadata) {
          pruneDeletedIssueFromParentChildrenCaches(qc, wsId, id, metadata);
        }
      }
      return { prevLists, prevMyLists, prevChildren, parentIssueIds, metadataById };
    },
    onError: (_err, _ids, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSuccess: (data, ids, ctx) => {
      if (data.deleted === ids.length) {
        const { forgetContext } = useRecentContextStore.getState();
        for (const id of ids) {
          forgetContext(wsId, { type: "issue", id });
          cleanupDeletedIssueCaches(qc, wsId, id, ctx?.metadataById.get(id));
        }
        return;
      }

      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
      for (const id of ids) {
        invalidateIssueScopedCaches(qc, wsId, id);
      }
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      invalidateDeletedIssueDependentCaches(qc, wsId);
    },
    onSettled: (_data, _err, _ids, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      if (ctx?.parentIssueIds && ctx.parentIssueIds.size > 0) {
        invalidateDeletedIssueParentCaches(qc, wsId, {
          parentIssueIds: Array.from(ctx.parentIssueIds),
        });
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
      suppressAgentIds,
    }: {
      content: string;
      type?: string;
      parentId?: string;
      attachmentIds?: string[];
      suppressAgentIds?: string[];
    }) => api.createComment(issueId, content, type, parentId, attachmentIds, suppressAgentIds),
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
        return sortTimelineEntriesAsc([...old, entry]);
      });
      // Posting a comment changes the trigger answer itself (the enqueued
      // task now dedupes follow-up triggers), so cached previews for this
      // issue are stale the moment the create lands.
      qc.invalidateQueries({ queryKey: issueKeys.commentTriggerPreview(issueId) });
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
    mutationFn: ({
      commentId,
      content,
      attachmentIds,
      suppressAgentIds,
    }: {
      commentId: string;
      content: string;
      attachmentIds: string[];
      suppressAgentIds?: string[];
    }) => api.updateComment(commentId, content, attachmentIds, suppressAgentIds),
    onMutate: async ({ commentId, content, attachmentIds }) => {
      await qc.cancelQueries({ queryKey: issueKeys.timeline(issueId) });
      const prev = qc.getQueryData<TimelineCache>(issueKeys.timeline(issueId));
      const kept = new Set(attachmentIds);
      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) =>
        old?.map((e) =>
          e.id === commentId
            ? { ...e, content, attachments: e.attachments?.filter((a) => kept.has(a.id)) }
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

// Every comment id in the same thread as `commentId` — the thread root plus
// every descendant. Mirrors the server's thread walk in
// ClearOtherThreadResolutions so the resolve optimistic update can clear sibling
// resolutions exactly as the backend will, instead of briefly showing two
// resolutions until the refetch settles.
function collectThreadCommentIds(
  entries: TimelineCache,
  commentId: string,
): Set<string> {
  const byId = new Map<string, TimelineEntry>();
  for (const e of entries) {
    if (e.type === "comment") byId.set(e.id, e);
  }
  // Walk up to the thread root (cycle-guarded against malformed parent chains).
  let rootId = commentId;
  const guard = new Set<string>();
  let cur = byId.get(commentId);
  while (cur?.parent_id && byId.has(cur.parent_id) && !guard.has(cur.id)) {
    guard.add(cur.id);
    rootId = cur.parent_id;
    cur = byId.get(cur.parent_id);
  }
  // Expand back down over the whole subtree.
  const childrenByParent = new Map<string, string[]>();
  for (const e of byId.values()) {
    if (e.parent_id) {
      const list = childrenByParent.get(e.parent_id) ?? [];
      list.push(e.id);
      childrenByParent.set(e.parent_id, list);
    }
  }
  const ids = new Set<string>([rootId]);
  const stack = [rootId];
  while (stack.length > 0) {
    const id = stack.pop()!;
    for (const child of childrenByParent.get(id) ?? []) {
      if (!ids.has(child)) {
        ids.add(child);
        stack.push(child);
      }
    }
  }
  return ids;
}

export function useResolveComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ commentId, resolved }: { commentId: string; resolved: boolean }) =>
      resolved ? api.resolveComment(commentId) : api.unresolveComment(commentId),
    onMutate: async ({ commentId, resolved }) => {
      await qc.cancelQueries({ queryKey: issueKeys.timeline(issueId) });
      const prev = qc.getQueryData<TimelineCache>(issueKeys.timeline(issueId));
      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) => {
        if (!old) return old;
        // Resolving makes this comment the sole resolution in its thread, so
        // mirror the server (ClearOtherThreadResolutions) and clear every other
        // resolution in the same thread. Without this the cache shows two
        // resolutions until the settle refetch, which is exactly the flash the
        // single-resolution fix removes. Unresolve only clears its own row.
        const threadIds = resolved ? collectThreadCommentIds(old, commentId) : null;
        return old.map((e) => {
          if (e.id === commentId) {
            return {
              ...e,
              resolved_at: resolved ? new Date().toISOString() : null,
              resolved_by_type: resolved ? e.resolved_by_type ?? null : null,
              resolved_by_id: resolved ? e.resolved_by_id ?? null : null,
            };
          }
          if (resolved && e.resolved_at && threadIds?.has(e.id)) {
            return { ...e, resolved_at: null, resolved_by_type: null, resolved_by_id: null };
          }
          return e;
        });
      });
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
