/**
 * Registry of workspace IDs with a DELETE in flight.
 *
 * Marked in useDeleteWorkspace.onMutate, cleared in onSettled. The workspace
 * list queryFn filters against it so a refetch that lands during the pending
 * window (realtime invalidation, reconnect recovery, explicit fetchQuery)
 * cannot write the not-yet-committed row back into the visible cache after
 * the optimistic removal.
 *
 * Module scope rather than React state because the queryFn runs outside the
 * component tree. A hard reload drops the registry, which matches server
 * truth: an uncommitted delete's workspace legitimately still exists.
 */
const pendingDeletes = new Set<string>();

export function markWorkspaceDeletePending(workspaceId: string) {
  pendingDeletes.add(workspaceId);
}

export function unmarkWorkspaceDeletePending(workspaceId: string) {
  pendingDeletes.delete(workspaceId);
}

/** Drop rows whose delete is still in flight from a fetched workspace list. */
export function omitPendingDeletes<T extends { id: string }>(list: T[]): T[] {
  if (pendingDeletes.size === 0) return list;
  return list.filter((w) => !pendingDeletes.has(w.id));
}
