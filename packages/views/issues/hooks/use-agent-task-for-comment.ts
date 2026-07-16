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
  // Only react to mutations on this issue's tasks cache — the hook
  // must not re-evaluate on every query mutation app-wide. The
  // prefix-match subscription is the same invalidation scope that
  // issueKeys.tasks(issueId) declares via WS `task:*` events.
  const subscribe = (onChange: () => void) =>
    queryClient.getQueryCache().subscribe((event) => {
      const key = event.query.queryKey as readonly unknown[];
      if (key[0] === "issues" && key[1] === "tasks" && key[2] === issueId) {
        onChange();
      }
    });
  const getSnapshot = (): AgentTask | null => {
    if (!sourceTaskId) return null;
    const tasks = queryClient.getQueryData<AgentTask[]>(issueKeys.tasks(issueId));
    if (!tasks) return null;
    // Workspace guard: even if the cache contains the source_task_id
    // (e.g. from a corrupted row or cross-issue reuse), reject mismatches
    // so the resolver returns null and the GC explainer fires instead
    // of the wrong-workspace transcript.
    const match = tasks.find(
      (t) => t.id === sourceTaskId && t.issue_id === issueId,
    );
    return match ?? null;
  };
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}