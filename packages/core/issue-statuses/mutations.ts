import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { compareIssueStatusEntries, issueStatusKeys } from "./queries";
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
 * No mutation invalidates the catalog on success. The refresh is the
 * `issue_status:changed` realtime event, which reaches the writing tab as well
 * as every other one, so a catalog edit costs each client exactly ONE catalog
 * read. Invalidating here too would make the admin who did the writing the
 * only client that reads it twice. (MUL-6458)
 *
 * That refetch is what CONVERGES the cache, and it is ordered correctly by
 * construction: the event is published after the write commits, so the refetch
 * it triggers can only read post-commit state.
 *
 * Which is why no mutation writes its RESPONSE BODY into the cache. A response
 * is authoritative for the row as of that write, not as of now — and the two
 * channels are independent, so a slow response can land after a refetch that
 * already picked up someone else's later write and quietly roll the catalog
 * back to a state nothing will correct. Every local write below is instead
 * something that stays true no matter what else has landed in between:
 *
 * - an optimistic field patch (rename, recolor, reorder),
 * - an insert of a row nobody else can have seen yet (create),
 * - a monotonic flag (archive — there is no unarchive endpoint, and the server
 *   refuses to edit an archived row, so setting it can never be stale).
 *
 * On FAILURE the invalidate stays, and it is not symmetry for its own sake: a
 * 409 usually means this client's catalog is the stale thing — someone else
 * took the name, or archived the row that was being dragged.
 */

function useCatalogCache() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const listKey = issueStatusKeys.list(wsId);

  const writeStatuses = (
    update: (statuses: IssueStatusEntry[]) => IssueStatusEntry[] | null,
    totalDelta = 0,
  ) => {
    qc.setQueryData<ListIssueStatusesResponse>(listKey, (old) => {
      if (!old) return old;
      const statuses = update(old.statuses);
      if (!statuses) return old;
      return {
        ...old,
        statuses: statuses.sort(compareIssueStatusEntries),
        total: old.total + totalDelta,
      };
    });
  };

  return {
    qc,
    listKey,
    /**
     * Adds a freshly created status to the cached catalog.
     *
     * Skipped when the id is already there, which is not just a duplicate
     * guard: the only way this client can already hold the row is a refetch
     * that read it back — possibly after someone else edited it — so the copy
     * in the cache is never older than the one in this response.
     *
     * Sorted, because `position` and `category` decide where a row renders and
     * appending would put a new In Review status below the Done rows.
     */
    insertEntry: (entry: IssueStatusEntry) => {
      // A response that failed schema validation degrades to an empty stub
      // (`parseWithFallback`). Writing it would put a blank row in the picker;
      // leaving the cache alone lets the realtime refetch supply the truth.
      if (!entry?.id) return;
      writeStatuses(
        (statuses) =>
          statuses.some((s) => s.id === entry.id) ? null : [...statuses, entry],
        1,
      );
    },
    /** Applies a field patch to one cached row, leaving the rest of it alone. */
    patchEntry: (id: string, patch: Partial<IssueStatusEntry>) => {
      writeStatuses((statuses) =>
        statuses.some((s) => s.id === id)
          ? statuses.map((s) => (s.id === id ? { ...s, ...patch } : s))
          : null,
      );
    },
    invalidate: () => {
      qc.invalidateQueries({ queryKey: issueStatusKeys.all(wsId) });
    },
  };
}

export function useCreateIssueStatus() {
  const { insertEntry, invalidate } = useCatalogCache();
  return useMutation({
    mutationFn: (data: CreateIssueStatusRequest) => api.createIssueStatus(data),
    onSuccess: insertEntry,
    onError: invalidate,
  });
}

/**
 * Optimistic rename / recolor / reorder — the same shape as `useUpdateLabel`.
 * Without it, dragging a row to reorder would snap back for the round-trip.
 */
export function useUpdateIssueStatus() {
  const { qc, listKey, patchEntry, invalidate } = useCatalogCache();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateIssueStatusRequest) =>
      api.updateIssueStatus(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: listKey });
      const previous = qc.getQueryData<ListIssueStatusesResponse>(listKey);
      patchEntry(id, data);
      return { previous };
    },
    // Nothing on success. The optimistic patch already shows the change, the
    // realtime refetch settles it, and installing the response snapshot here
    // is exactly what could roll a concurrent edit back (see the note above).
    //
    // A rename also deliberately does NOT invalidate the issues scope. An issue
    // row stores the status KEY; its name and color are resolved from this
    // catalog at render time (`useStatusLabel`, `colorOf`), so no cached issue
    // field can go stale here — refreshing the catalog is what repaints the
    // boards. The old invalidate refetched every board, list and table in the
    // workspace to change one word. (MUL-6458)
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) qc.setQueryData(listKey, ctx.previous);
      invalidate();
    },
  });
}

/**
 * Archives a custom status. Deliberately NOT optimistic: the server refuses to
 * archive a built-in, and a row silently vanishing before that refusal arrives
 * would read as success.
 */
export function useArchiveIssueStatus() {
  const { patchEntry, invalidate } = useCatalogCache();
  return useMutation({
    mutationFn: (id: string) => api.archiveIssueStatus(id),
    // Only `archived_at`, never the whole returned row. Archiving is terminal —
    // there is no unarchive, and the server refuses to edit an archived status
    // — so the flag stays true whatever else landed in the cache meanwhile,
    // while a full snapshot would revert a rename that raced this archive.
    //
    // The row itself stays: issues still sitting on it resolve their name,
    // color and category through it. `activeStatuses` is what hides it from
    // pickers.
    onSuccess: (entry) => {
      if (!entry?.id) return;
      patchEntry(entry.id, { archived_at: entry.archived_at });
    },
    onError: invalidate,
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
  const { qc, listKey, invalidate } = useCatalogCache();
  return useMutation({
    mutationFn: ({
      category,
      ordered,
    }: {
      category: IssueStatusCategory;
      ordered: IssueStatusEntry[];
    }) => api.reorderIssueStatuses(category, ordered.map((entry) => entry.id)),
    onMutate: async ({ ordered }) => {
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
      return { previous };
    },
    // Reorder answers with the FULL catalog, and that is precisely why the
    // response is not installed: a whole-table snapshot that arrives late
    // overwrites every row, so ONE slow reorder response would roll back every
    // concurrent edit the refetch had already picked up.
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) qc.setQueryData(listKey, ctx.previous);
      invalidate();
    },
  });
}
