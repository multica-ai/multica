import { normalizeStatusPatch } from "./status-category";
import { hashKey, useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";
import { projectKeys } from "../projects/queries";
import { inboxKeys } from "../inbox/queries";
import {
  applyIssueChange,
  invalidateIssueDerivatives,
  invalidateLastActivitySortedIssueLists,
  invalidateStaleListKeys,
  rollbackIssueChange,
  type IssueFlatCache,
  type IssueTableRowCache,
} from "./cache-coordinator";
import { issueChangedDims } from "./surface/membership";
import { addIssueToBuckets } from "./cache-helpers";
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
import type { InboxItem, Issue, IssueReaction } from "../types";
import type {
  CreateCommentSubIssueManualRequest,
  CreateIssueRequest,
  ListIssuesCache,
  MoveIssueRequest,
  UpdateIssueRequest,
} from "../types";
import type { TimelineEntry, IssueSubscriber, Reaction } from "../types";
import { sortTimelineEntriesAsc } from "./timeline-sort";
import {
  onIssueAuxiliaryRevision,
  invalidateIssueOwnerProjections,
  reconcileIssueFullSnapshotRevision,
} from "./ws-updaters";

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

export type UpdateIssueMutationInput = {
  id: string;
  /**
   * Present only for drag/drop. `position` remains in the optimistic patch,
   * while the request sent to the server contains relative anchors instead.
   */
  move_intent?: Pick<MoveIssueRequest, "before_id" | "after_id">;
} & UpdateIssueRequest;

// ---------------------------------------------------------------------------
// Per-status pagination
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Issue CRUD
// ---------------------------------------------------------------------------

function useIssueCreateMutation<TVariables>(
  mutationFn: (variables: TVariables) => Promise<Issue>,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn,
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
      qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}

export function useCreateIssue() {
  return useIssueCreateMutation((data: CreateIssueRequest) => api.createIssue(data));
}

export function useCreateCommentSubIssue() {
  return useIssueCreateMutation(({
    anchorCommentId,
    data,
  }: {
    anchorCommentId: string;
    data: CreateCommentSubIssueManualRequest;
  }) => api.createCommentSubIssue(anchorCommentId, data));
}

export function useUpdateIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, move_intent: moveIntent, ...data }: UpdateIssueMutationInput) => {
      if (!moveIntent) return api.updateIssue(id, data);
      const { position: _optimisticPosition, ...target } = data;
      return api.moveIssue(id, { ...target, ...moveIntent });
    },
    onMutate: ({ id, move_intent: _moveIntent, ...data }) => {
      // suppress_run / handoff_note are write-time control fields, not Issue
      // columns. description_base is merge metadata, while description itself
      // is resolved against that base on the server and therefore is not safe
      // to predict optimistically. Keep the authoritative raw description in
      // cache so hidden channel-media markers remain available as the base for
      // a rapid follow-up edit. mutationFn still sends the full payload.
      const {
        suppress_run: _suppressRun,
        handoff_note: _handoffNote,
        description: _description,
        description_base: _descriptionBase,
        title_base: _titleBase,
        expected_revision: _expectedRevision,
        ...patch
      } = data;
      // Fire-and-forget cancelQueries — keeps onMutate synchronous so the
      // cache update happens in the same tick as mutate(). Awaiting would
      // yield to the event loop, letting @dnd-kit reset its visual state
      // before the optimistic update lands.
      qc.cancelQueries({ queryKey: issueKeys.list(wsId) });
      qc.cancelQueries({ queryKey: issueKeys.myAll(wsId) });
      qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.cancelQueries({ queryKey: issueKeys.tableAll(wsId) });
      if (patch.status !== undefined || patch.priority !== undefined) {
        qc.cancelQueries({ queryKey: inboxKeys.all(wsId) });
      }
      const prevDetail = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));
      // The coordinator owns the cross-cache rules: surgical patch/rebucket
      // where the card is loaded and still belongs, surgical REMOVE where the
      // change moves it off a filtered surface, stale-key bookkeeping where
      // the server result may have drifted (invalidated on settle, not here —
      // a mid-flight refetch would stomp the optimistic state).
      const change = applyIssueChange(qc, wsId, id, patch as Partial<Issue>, {
        changed: issueChangedDims(patch, prevDetail),
        baseIssue: prevDetail,
      });

      // Resolve parent_issue_id from the freshest source so we can keep the
      // parent's children cache in sync (used by the parent issue's
      // sub-issues list). Falls back to scanning loaded children caches —
      // when the user navigates straight to a parent's detail page, the
      // child may live only there, not in detail/list.
      let parentId: string | null =
        prevDetail?.parent_issue_id ??
        change.prevIssue?.parent_issue_id ??
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

      if (parentId) {
        // When the write re-parents this issue away from `parentId` (detach
        // to standalone, or move under a different parent), prune it from the
        // old parent's children cache. The parent's sub-issues list renders
        // that array directly, so a bare patch to parent_issue_id: null would
        // leave an orphaned row in the list until the settle refetch lands.
        // onError restores prevChildren, so the prune rolls back on failure.
        const detachedFromParent =
          Object.prototype.hasOwnProperty.call(patch, "parent_issue_id") &&
          patch.parent_issue_id !== parentId;
        qc.setQueryData<Issue[]>(
          issueKeys.children(wsId, parentId),
          (old) =>
            detachedFromParent
              ? old?.filter((c) => c.id !== id)
              : old?.map((c) => (c.id === id ? { ...c, ...normalizeStatusPatch(patch) } : c)),
        );
      }
      return { change, prevChildren, parentId, id };
    },
    onError: (_err, vars, ctx) => {
      if (ctx) {
        rollbackIssueChange(qc, wsId, ctx.id, ctx.change);
      }
      if (ctx?.parentId && ctx.prevChildren !== undefined) {
        qc.setQueryData(
          issueKeys.children(wsId, ctx.parentId),
          ctx.prevChildren,
        );
      }
      // A remote revision may have landed through WS after onMutate captured
      // its rollback snapshot. Restoring that snapshot on any failed request
      // can therefore transiently put an older entity back into cache; refresh
      // every loaded owner projection after rollback. A revision conflict is
      // the common case, but transport/5xx failures have the same interleave.
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
    },
    onSuccess: (serverIssue, vars) => {
      // Reconcile with the authoritative server entity by patching the one card
      // in place — NOT by invalidating + refetching the list. The list refetch
      // is what made a successful move flicker: the optimistic card was already
      // in the right place, then the refetch replaced the whole column and the
      // card re-landed. updateIssue returns the full issue and a position update
      // touches only that row, so a surgical patch is the authoritative
      // reconcile and is a visual no-op when the optimistic value matched.
      //
      // baseIssue = serverIssue: membership moves were already handled
      // optimistically; against the post-write entity the changed dims come
      // out false unless the server coerced a different value, so this pass
      // is the plain surgical patch it always was.
      const {
        suppress_run: _suppressRun,
        handoff_note: _handoffNote,
        description_base: _descriptionBase,
        move_intent: _moveIntent,
        id: _id,
        ...intent
      } = vars;
      // Drop `properties` from the reconcile payload: the bag is owned by the
      // property mutation pipeline (single-key atomic writes + its own
      // optimistic state). An UpdateIssue snapshot taken before a concurrent
      // property write resolves would otherwise overwrite the newer bag
      // (clean-room review F3 response-ordering race).
      const { properties: _staleBag, ...reconcilable } = serverIssue;
      const reconcile = applyIssueChange(qc, wsId, serverIssue.id, reconcilable as typeof serverIssue, {
        changed: issueChangedDims(intent, serverIssue),
        baseIssue: serverIssue,
        // The HTTP response can arrive after a newer WS event. Reconcile the
        // committed snapshot only into projections that have not already
        // advanced beyond it; otherwise an older successful response would
        // undo a later remote write in cache.
        acceptCurrent: (current) =>
          current.revision === undefined ||
          (serverIssue.revision !== undefined &&
            serverIssue.revision > current.revision),
      });
      reconcileIssueFullSnapshotRevision(
        qc,
        wsId,
        serverIssue.id,
        serverIssue.revision,
      );
      // The server has committed — safe to flush any drift it reported now.
      invalidateStaleListKeys(qc, reconcile.staleKeys);
    },
    onSettled: (_data, _err, vars, ctx) => {
      // The issue's own list + detail caches are reconciled surgically in
      // onSuccess / onError, so they are deliberately NOT invalidated here — a
      // full-list refetch on settle is what made drags flicker. Only aggregate
      // caches that cannot be patched from a single issue are refreshed, plus
      // the specific list keys the coordinator flagged as drifted (unknown
      // membership, enter/leave beyond the loaded window, bucket-count drift).
      // Those stale keys are the surgical replacement for the old blanket
      // "invalidate myAll on project move" safety net (MUL-3669 / #4548): the
      // old project's loaded list already had the card removed in onMutate,
      // and only genuinely undecidable lists refetch here.
      invalidateIssueDerivatives(qc, wsId, {
        statusOrProjectChanged:
          vars.status !== undefined ||
          Object.prototype.hasOwnProperty.call(vars, "project_id"),
      });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
      if (ctx) {
        invalidateStaleListKeys(qc, ctx.change.staleKeys);
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
        qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) }),
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
      const prevFlatLists = qc.getQueriesData<IssueFlatCache>({
        queryKey: issueKeys.flatAll(wsId),
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
      return {
        id,
        metadata,
        prevLists,
        prevMyLists,
        prevFlatLists,
        prevDetail,
        prevChildren,
      };
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
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
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
      qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
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
      // Control and description-merge fields are not safe optimistic cache
      // patches. The server resolves description against description_base, so
      // preserve the authoritative raw description (including media markers)
      // until a refetch returns the committed result.
      const {
        suppress_run: _suppressRun,
        handoff_note: _handoffNote,
        description: _description,
        description_base: _descriptionBase,
        ...patch
      } = updates;
      await qc.cancelQueries({ queryKey: issueKeys.list(wsId) });
      await qc.cancelQueries({ queryKey: issueKeys.myAll(wsId) });
      await qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) });
      await qc.cancelQueries({ queryKey: issueKeys.tableAll(wsId) });
      if (patch.status !== undefined || patch.priority !== undefined) {
        await qc.cancelQueries({ queryKey: inboxKeys.all(wsId) });
      }

      // Run every issue through the coordinator — the same rules table the
      // single-issue update uses, so a batch edit patches/removes across the
      // workspace board AND every filtered myList surface identically.
      // Snapshots are first-wins per cache key: after the first issue's
      // application a cache already carries partial patches, so only the
      // first snapshot per key is pristine for rollback.
      const prevListByHash = new Map<string, [QueryKey, ListIssuesCache]>();
      const prevFlatListByHash = new Map<string, [QueryKey, IssueFlatCache]>();
      const prevTableRowByHash = new Map<
        string,
        [QueryKey, IssueTableRowCache]
      >();
      const prevDetailById = new Map<string, Issue>();
      let prevInboxList: InboxItem[] | undefined;
      let prevArchivedInboxList: InboxItem[] | undefined;
      const staleKeys: QueryKey[] = [];
      for (const id of ids) {
        const base = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));
        const change = applyIssueChange(qc, wsId, id, patch as Partial<Issue>, {
          changed: issueChangedDims(patch, base),
          baseIssue: base,
        });
        for (const [key, snapshot] of change.prevLists) {
          const hash = hashKey(key);
          if (!prevListByHash.has(hash)) prevListByHash.set(hash, [key, snapshot]);
        }
        for (const [key, snapshot] of change.prevFlatLists) {
          const hash = hashKey(key);
          if (!prevFlatListByHash.has(hash)) {
            prevFlatListByHash.set(hash, [key, snapshot]);
          }
        }
        for (const [key, snapshot] of change.prevTableRows) {
          const hash = hashKey(key);
          if (!prevTableRowByHash.has(hash)) {
            prevTableRowByHash.set(hash, [key, snapshot]);
          }
        }
        if (change.prevDetail) prevDetailById.set(id, change.prevDetail);
        if (prevInboxList === undefined && change.prevInboxList !== undefined) {
          prevInboxList = change.prevInboxList;
        }
        if (
          prevArchivedInboxList === undefined &&
          change.prevArchivedInboxList !== undefined
        ) {
          prevArchivedInboxList = change.prevArchivedInboxList;
        }
        staleKeys.push(...change.staleKeys);
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
          old?.map((c) => (idSet.has(c.id) ? { ...c, ...normalizeStatusPatch(patch) } : c)),
        );
      }

      return {
        prevLists: [...prevListByHash.values()],
        prevFlatLists: [...prevFlatListByHash.values()],
        prevTableRows: [...prevTableRowByHash.values()],
        prevDetailById,
        prevInboxList,
        prevArchivedInboxList,
        staleKeys,
        prevChildren,
        affectedParentIds,
      };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevTableRows) {
        for (const [key, snapshot] of ctx.prevTableRows) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevDetailById) {
        for (const [id, snapshot] of ctx.prevDetailById) {
          qc.setQueryData(issueKeys.detail(wsId, id), snapshot);
        }
      }
      if (ctx?.prevInboxList !== undefined) {
        qc.setQueryData(inboxKeys.list(wsId), ctx.prevInboxList);
      }
      if (ctx?.prevArchivedInboxList !== undefined) {
        qc.setQueryData(
          inboxKeys.archived(wsId),
          ctx.prevArchivedInboxList,
        );
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSettled: (_data, _err, _vars, ctx) => {
      // Deliberately NOT invalidating issueKeys.list / myAll here: the onMutate
      // pass above is a complete surgical reconcile for the loaded bucketed
      // boards, so a full-board refetch on settle would only re-introduce the
      // flicker the single-issue update already removed. Aggregate / grouped
      // caches that cannot be recomputed from a single-issue patch are
      // refreshed below, plus the specific keys the coordinator flagged as
      // drifted — the surgical replacement for the old blanket "invalidate
      // myAll on project move" safety net (MUL-3669 / #4548).
      invalidateIssueDerivatives(qc, wsId, {
        statusOrProjectChanged:
          _vars.updates.status !== undefined ||
          Object.prototype.hasOwnProperty.call(_vars.updates, "project_id"),
      });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
      if (ctx) {
        invalidateStaleListKeys(qc, ctx.staleKeys);
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
        qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) }),
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
      const prevFlatLists = qc.getQueriesData<IssueFlatCache>({
        queryKey: issueKeys.flatAll(wsId),
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
      return {
        prevLists,
        prevMyLists,
        prevFlatLists,
        prevChildren,
        parentIssueIds,
        metadataById,
      };
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
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
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
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
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
      qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
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
  const wsId = useWorkspaceId();
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
      if (comment.issue_revision) {
        onIssueAuxiliaryRevision(qc, wsId, issueId, comment.issue_revision);
      } else {
        invalidateIssueOwnerProjections(qc, wsId, issueId);
      }
      invalidateLastActivitySortedIssueLists(qc, wsId);
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
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      commentId,
      content,
      attachmentIds,
      suppressAgentIds,
      contentBase,
      expectedRevision,
    }: {
      commentId: string;
      content: string;
      attachmentIds: string[];
      suppressAgentIds?: string[];
      contentBase?: string;
      expectedRevision?: number;
    }) => api.updateComment(commentId, content, attachmentIds, suppressAgentIds, contentBase, expectedRevision),
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
    onSuccess: (comment) => {
      if (comment.issue_revision) {
        onIssueAuxiliaryRevision(qc, wsId, issueId, comment.issue_revision);
      } else {
        // Mixed-version fallback: an older backend may have committed the
        // aggregate revision without returning it.
        invalidateIssueOwnerProjections(qc, wsId, issueId);
      }
      invalidateLastActivitySortedIssueLists(qc, wsId);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

export function useDeleteComment(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
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
    onSuccess: () => {
      // The endpoint remains 204 for compatibility, so the local caller has
      // no body carrying issue_revision. The realtime event will narrow this
      // with its revision when connected; this is the no-WS safety net.
      invalidateIssueOwnerProjections(qc, wsId, issueId);
      invalidateLastActivitySortedIssueLists(qc, wsId);
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
  const wsId = useWorkspaceId();
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
    onSuccess: (reaction) => {
      onIssueAuxiliaryRevision(qc, wsId, issueId, reaction?.issue_revision);
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

/**
 * Leave an issue AND its whole sub-tree (MUL-5483). Not optimistic: it writes
 * to an unknown number of other issues' subscriber lists, so there is nothing
 * determinate to patch — invalidate every subscriber query instead and let the
 * server be the source of truth.
 */
export function useUnsubscribeFromIssueSubtree(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      userId,
      userType,
    }: {
      userId: string;
      userType: "member" | "agent";
    }) => {
      await api.unsubscribeFromIssueSubtree(issueId, userId, userType);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.subscribersAll() });
    },
  });
}
