"use client";

import { useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types/agent";

/**
 * Sync, cache-only lookup for the AgentTask that produced a given comment.
 * Returns `null` when the cache has not been populated yet (sidebar not
 * mounted on this view), when the comment has no `source_task_id`, or when
 * the cache has been GC'd and no matching task id is found.
 *
 * Implementation notes:
 * - Pure synchronous read: no `useQuery`, no network, no event subscription.
 *   Memo stability of CommentCard depends on this returning a stable
 *   reference across renders when the underlying cache is unchanged —
 *   `useSyncExternalStore` enforces that, and `Array.find` returns the same
 *   AgentTask object reference for the same tasks array reference (TanStack
 *   Query preserves object identity for cache writes that don't change a
 *   given entry).
 * - Workspace scoping is handled upstream by `listTasksByIssue`: the
 *   `issueKeys.tasks(issueId)` cache only contains tasks the workspace can
 *   read. Cross-workspace leakage is therefore impossible by construction.
 * - Cache is the trusted validation boundary — `api.listTasksByIssue`
 *   parses with `parseWithFallback` before populating, per the project's
 *   API compatibility rule. Re-validating here would defeat memo stability
 *   (`useSyncExternalStore` requires snapshot identity preservation) for
 *   no security gain.
 */
export function useAgentTaskForComment(
  issueId: string,
  sourceTaskId: string | null | undefined,
): AgentTask | null {
  const queryClient = useQueryClient();
  const subscribe = (onChange: () => void) =>
    queryClient.getQueryCache().subscribe(() => onChange());
  const getSnapshot = (): AgentTask | null => {
    if (!sourceTaskId) return null;
    const tasks = queryClient.getQueryData<AgentTask[]>(issueKeys.tasks(issueId));
    if (!tasks) return null;
    return tasks.find((t) => t.id === sourceTaskId) ?? null;
  };
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}