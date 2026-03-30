"use client";

import { useCallback, useEffect, useMemo } from "react";
import { toast } from "sonner";
import { ChevronRight } from "lucide-react";
import type { IssueStatus } from "@/shared/types";
import { Skeleton } from "@/components/ui/skeleton";
import { useIssueStore } from "@/features/issues/store";
import { useIssueViewStore } from "@/features/issues/stores/view-store";
import { useWorkspaceStore } from "@/features/workspace";
import { WorkspaceAvatar } from "@/features/workspace";
import { api } from "@/shared/api";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { IssuesHeader } from "./issues-header";
import { BoardView } from "./board-view";
import { ListView } from "./list-view";
import { BatchActionToolbar } from "./batch-action-toolbar";

const BOARD_STATUSES: IssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
];

export function IssuesPage() {
  const allIssues = useIssueStore((s) => s.issues);
  const loading = useIssueStore((s) => s.loading);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const viewMode = useIssueViewStore((s) => s.viewMode);
  const statusFilters = useIssueViewStore((s) => s.statusFilters);
  const priorityFilters = useIssueViewStore((s) => s.priorityFilters);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode]);

  const issues = useMemo(() => {
    return allIssues.filter((issue) => {
      if (statusFilters.length > 0 && !statusFilters.includes(issue.status))
        return false;
      if (
        priorityFilters.length > 0 &&
        !priorityFilters.includes(issue.priority)
      )
        return false;
      return true;
    });
  }, [allIssues, statusFilters, priorityFilters]);

  const visibleStatuses = useMemo(() => {
    if (statusFilters.length > 0)
      return BOARD_STATUSES.filter((s) => statusFilters.includes(s));
    return BOARD_STATUSES;
  }, [statusFilters]);

  const hiddenStatuses = useMemo(() => {
    return BOARD_STATUSES.filter((s) => !visibleStatuses.includes(s));
  }, [visibleStatuses]);

  const handleMoveIssue = useCallback(
    (issueId: string, newStatus: IssueStatus, newPosition?: number) => {
      // Auto-switch to manual sort so drag ordering is preserved
      if (useIssueViewStore.getState().sortBy !== "position") {
        useIssueViewStore.getState().setSortBy("position");
        useIssueViewStore.getState().setSortDirection("asc");
      }

      const updates: Partial<{ status: IssueStatus; position: number }> = {
        status: newStatus,
      };
      if (newPosition !== undefined) updates.position = newPosition;

      useIssueStore.getState().updateIssue(issueId, updates);

      api.updateIssue(issueId, updates).catch(() => {
        toast.error("Failed to move issue");
        api.listIssues({ limit: 200 }).then((res) => {
          useIssueStore.getState().setIssues(res.issues);
        });
      });
    },
    []
  );

  if (loading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-8 w-24" />
        </div>
        <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="flex min-w-52 flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-20" />
              <Skeleton className="h-24 w-full rounded-lg" />
              <Skeleton className="h-24 w-full rounded-lg" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Header 1: Workspace breadcrumb */}
      <div className="flex h-12 shrink-0 items-center gap-1.5 border-b px-4">
        <WorkspaceAvatar name={workspace?.name ?? "W"} size="sm" />
        <span className="text-sm text-muted-foreground">
          {workspace?.name ?? "Workspace"}
        </span>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="text-sm font-medium">Issues</span>
      </div>

      {/* Header 2: View toggle + filters */}
      <IssuesHeader />

      {/* Content: scrollable */}
      <div className="flex flex-col flex-1 min-h-0">
        {viewMode === "board" ? (
          <BoardView
            issues={issues}
            allIssues={allIssues}
            visibleStatuses={visibleStatuses}
            hiddenStatuses={hiddenStatuses}
            onMoveIssue={handleMoveIssue}
          />
        ) : (
          <ListView issues={issues} visibleStatuses={visibleStatuses} />
        )}
      </div>
      {viewMode === "list" && <BatchActionToolbar />}
    </div>
  );
}
