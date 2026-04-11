"use client";

import { useState, useCallback, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { TaskFileTreePayload } from "@multica/core/types";
import {
  taskFileTreeOptions,
  taskFileContentOptions,
  taskFileDiffOptions,
  issueKeys,
  type TaskFileTreeData,
} from "@multica/core/issues/queries";
import { useWorktreeViewStore } from "@multica/core/issues/stores";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";

/**
 * Hook for browsing an agent task's worktree files.
 * Tree data arrives via `task:file_tree` WS events (with a REST fallback
 * on mount). File content is fetched on demand when a file is selected.
 * When `agentId` is provided, the selected file is persisted per (issue, agent)
 * and restored when the hook remounts with the same identifiers.
 */
export function useTaskFileTree(
  issueId: string,
  taskId: string | undefined,
  agentId?: string,
  /** When true, fetch a diff for the selected file instead of raw content. */
  diffMode: boolean = false,
) {
  const qc = useQueryClient();
  const setPersistedPath = useWorktreeViewStore((s) => s.setSelectedPath);
  // Read the persisted path lazily on each agent change (not via selector —
  // we don't want re-renders when another (issue, agent) pair updates).
  const [selectedPath, setSelectedPath] = useState<string | null>(() => {
    if (!agentId) return null;
    const k = `${issueId}:${agentId}`;
    return useWorktreeViewStore.getState().entries[k]?.selectedPath ?? null;
  });

  // Restore selection when switching to a different agent worktree.
  useEffect(() => {
    if (!agentId) {
      setSelectedPath(null);
      return;
    }
    const k = `${issueId}:${agentId}`;
    const restored =
      useWorktreeViewStore.getState().entries[k]?.selectedPath ?? null;
    setSelectedPath(restored);
  }, [issueId, agentId]);

  // File tree — populated via WS events
  const { data: fileTreeData } = useQuery(
    taskFileTreeOptions(issueId, taskId ?? ""),
  );

  // File content — fetched on demand when a file is selected (only when
  // not in diff mode, to avoid fetching both).
  const { data: fileContent, isLoading: contentLoading } = useQuery({
    ...taskFileContentOptions(issueId, taskId ?? "", selectedPath ?? ""),
    enabled: !!selectedPath && !diffMode,
  });

  // File diff — fetched when a file is selected in diff mode.
  const { data: fileDiff, isLoading: diffLoading } = useQuery({
    ...taskFileDiffOptions(issueId, taskId ?? "", selectedPath ?? ""),
    enabled: !!selectedPath && diffMode,
  });

  // Listen for task:file_tree WS events and inject into query cache
  useWSEvent(
    "task:file_tree",
    useCallback(
      (payload: unknown) => {
        const p = payload as TaskFileTreePayload;
        if (p.issue_id !== issueId || (taskId && p.task_id !== taskId)) return;
        qc.setQueryData<TaskFileTreeData>(
          issueKeys.taskFileTree(issueId, p.task_id),
          {
            tree: p.tree,
            git_status: p.git_status,
          },
        );
        // Invalidate any cached file contents / diffs since the tree changed
        qc.invalidateQueries({
          queryKey: ["issues", "taskFileContent", issueId, p.task_id],
        });
        qc.invalidateQueries({
          queryKey: ["issues", "taskFileDiff", issueId, p.task_id],
        });
      },
      [qc, issueId, taskId],
    ),
  );

  // On reconnect, we can't refetch the tree (no REST endpoint),
  // but we should invalidate file content / diffs so stale data isn't shown
  useWSReconnect(
    useCallback(() => {
      if (taskId) {
        qc.invalidateQueries({
          queryKey: ["issues", "taskFileContent", issueId, taskId],
        });
        qc.invalidateQueries({
          queryKey: ["issues", "taskFileDiff", issueId, taskId],
        });
      }
    }, [qc, issueId, taskId]),
  );

  const selectFile = useCallback(
    (path: string | null) => {
      setSelectedPath(path);
      if (agentId) setPersistedPath(issueId, agentId, path);
    },
    [agentId, issueId, setPersistedPath],
  );

  return {
    tree: fileTreeData?.tree ?? null,
    gitStatus: fileTreeData?.git_status ?? {},
    selectedPath,
    selectFile,
    fileContent: fileContent ?? null,
    contentLoading,
    fileDiff: fileDiff ?? null,
    diffLoading,
  };
}
