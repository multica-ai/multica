/**
 * Registry of workspace IDs whose DELETE was initiated by THIS client.
 *
 * Marked in useDeleteWorkspace.onMutate. The realtime `workspace:deleted`
 * handler checks it and no-ops for self-initiated deletes: the mutation flow
 * owns storage cleanup and navigation (both run after the DELETE resolves),
 * and letting the handler react too would race that flow's navigation with
 * its own full-page relocate.
 *
 * Lifted only when the DELETE fails — the workspace still exists, so a later
 * external delete of the same ID must be handled normally. On success the ID
 * is gone for good, and keeping the mark suppresses the WS echo of our own
 * delete no matter when it arrives.
 *
 * Module scope rather than React state because the realtime handler runs
 * outside the component tree. Per-tab by construction: other tabs/devices
 * have an empty registry, so their handlers process the event normally.
 */
const pendingDeletes = new Set<string>();

export function markWorkspaceDeletePending(workspaceId: string) {
  pendingDeletes.add(workspaceId);
}

export function unmarkWorkspaceDeletePending(workspaceId: string) {
  pendingDeletes.delete(workspaceId);
}

/** True if this client initiated a DELETE for the workspace. */
export function isWorkspaceDeletePending(workspaceId: string): boolean {
  return pendingDeletes.has(workspaceId);
}
