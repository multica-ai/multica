import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { compareIssueStatusEntries, issueStatusKeys } from "./queries";
import { issueKeys } from "../issues/queries";
import type {
  CreateIssueStatusRequest,
  IssueStatusCategory,
  IssueStatusEntry,
  ListIssueStatusesResponse,
  UpdateIssueStatusRequest,
} from "../types";

/**
 * Catalog mutations (MUL-6243).
 *
 * The catalog query is cached for 5 minutes and read by every surface that
 * renders a status label, so each mutation invalidates it explicitly rather
 * than waiting for staleness. A rename or recolor also invalidates the issues
 * scope: a status's name is resolved from the catalog at render time, but the
 * board/list caches hold the rows whose labels have to re-render.
 */

function useCatalogInvalidation() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return {
    qc,
    wsId,
    invalidate: () => {
      qc.invalidateQueries({ queryKey: issueStatusKeys.all(wsId) });
    },
  };
}

export function useCreateIssueStatus() {
  const { qc, wsId, invalidate } = useCatalogInvalidation();
  return useMutation({
    mutationFn: (data: CreateIssueStatusRequest) => api.createIssueStatus(data),
    onSuccess: (entry) => {
      qc.setQueryData<ListIssueStatusesResponse>(issueStatusKeys.list(wsId), (old) =>
        old && !old.statuses.some((s) => s.id === entry.id)
          ? { ...old, statuses: [...old.statuses, entry], total: old.total + 1 }
          : old,
      );
    },
    onSettled: invalidate,
  });
}

/**
 * Optimistic rename / recolor / reorder — the same shape as `useUpdateLabel`.
 * Without it, dragging a row to reorder would snap back for the round-trip.
 */
export function useUpdateIssueStatus() {
  const { qc, wsId, invalidate } = useCatalogInvalidation();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateIssueStatusRequest) =>
      api.updateIssueStatus(id, data),
    onMutate: async ({ id, ...data }) => {
      const listKey = issueStatusKeys.list(wsId);
      await qc.cancelQueries({ queryKey: listKey });
      const previous = qc.getQueryData<ListIssueStatusesResponse>(listKey);
      qc.setQueryData<ListIssueStatusesResponse>(listKey, (old) =>
        old
          ? {
              ...old,
              statuses: old.statuses.map((s) => (s.id === id ? { ...s, ...data } : s)),
            }
          : old,
      );
      return { previous, listKey };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous && ctx.listKey) qc.setQueryData(ctx.listKey, ctx.previous);
    },
    onSettled: () => {
      invalidate();
      // Issue rows render the catalog's name/color, so a rename has to reach
      // the cached lists too.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

/**
 * Archives a custom status. Deliberately NOT optimistic: the server refuses to
 * archive a built-in, and a row silently vanishing before that refusal arrives
 * would read as success.
 */
export function useArchiveIssueStatus() {
  const { invalidate } = useCatalogInvalidation();
  return useMutation({
    mutationFn: (id: string) => api.archiveIssueStatus(id),
    onSettled: invalidate,
  });
}

/**
 * Commits a drag-reorder within ONE category.
 *
 * Sent as a single request, not a PATCH per row: a sequence of writes is not
 * atomic, so a row rejected part-way (an archived status, a concurrent archive)
 * would leave the rows before it already reordered while the caller is told the
 * whole operation failed. `ordered` is that category's ACTIVE custom statuses;
 * the server assigns positions from 1 because the category's built-in is seeded
 * at 0 and never moves.
 */
export function useReorderIssueStatuses() {
  const { qc, wsId, invalidate } = useCatalogInvalidation();
  return useMutation({
    mutationFn: ({
      category,
      ordered,
    }: {
      category: IssueStatusCategory;
      ordered: IssueStatusEntry[];
    }) => api.reorderIssueStatuses(category, ordered.map((entry) => entry.id)),
    onMutate: async ({ ordered }) => {
      const listKey = issueStatusKeys.list(wsId);
      await qc.cancelQueries({ queryKey: listKey });
      const previous = qc.getQueryData<ListIssueStatusesResponse>(listKey);
      const positionById = new Map(ordered.map((e, index) => [e.id, index + 1]));
      qc.setQueryData<ListIssueStatusesResponse>(listKey, (old) =>
        old
          ? {
              ...old,
              // Re-sorted, not just re-positioned: consumers render the array in
              // order, so writing positions alone would leave the drag visually
              // undone until the refetch lands.
              statuses: old.statuses
                .map((s) =>
                  positionById.has(s.id) ? { ...s, position: positionById.get(s.id)! } : s,
                )
                .sort(compareIssueStatusEntries),
            }
          : old,
      );
      return { previous, listKey };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous && ctx.listKey) qc.setQueryData(ctx.listKey, ctx.previous);
    },
    onSettled: invalidate,
  });
}
