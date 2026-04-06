"use client";

import { useCallback, useEffect, useMemo } from "react";
import { toast } from "sonner";
import { ChevronRight, ListTodo, Plus } from "lucide-react";
import type { IssueStatus } from "@/shared/types";
import { Skeleton } from "@/components/ui/skeleton";
import { useIssueStore } from "@/features/issues/store";
import { useIssueViewStore, initFilterWorkspaceSync } from "@/features/issues/stores/view-store";
import { useIssuesScopeStore } from "@/features/issues/stores/issues-scope-store";
import { ViewStoreProvider } from "@/features/issues/stores/view-store-context";
import { filterIssues } from "@/features/issues/utils/filter";
import { BOARD_STATUSES } from "@/features/issues/config";
import { useWorkspaceStore } from "@/features/workspace";
import { WorkspaceAvatar } from "@/features/workspace";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { api } from "@/shared/api";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { useModalStore } from "@/features/modals";
import { Button } from "@/components/ui/button";
import { IssuesHeader } from "./issues-header";
import { BoardView } from "./board-view";
import { ListView } from "./list-view";
import { BatchActionToolbar } from "./batch-action-toolbar";

export function IssuesPage() {
  const allIssues = useIssueStore((s) => s.issues);
  const loading = useIssueStore((s) => s.loading);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const scope = useIssuesScopeStore((s) => s.scope);
  const viewMode = useIssueViewStore((s) => s.viewMode);
  const statusFilters = useIssueViewStore((s) => s.statusFilters);
  const priorityFilters = useIssueViewStore((s) => s.priorityFilters);
  const assigneeFilters = useIssueViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useIssueViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useIssueViewStore((s) => s.creatorFilters);

  useEffect(() => {
    initFilterWorkspaceSync();
  }, []);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode, scope]);

  // Scope pre-filter: narrow by assignee type
  const scopedIssues = useMemo(() => {
    if (scope === "members")
      return allIssues.filter((i) => i.assignee_type === "member");
    if (scope === "agents")
      return allIssues.filter((i) => i.assignee_type === "agent");
    return allIssues;
  }, [allIssues, scope]);

  const issues = useMemo(
    () => filterIssues(scopedIssues, { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters }),
    [scopedIssues, statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters],
  );

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
      const viewState = useIssueViewStore.getState();
      if (viewState.sortBy !== "position") {
        viewState.setSortBy("position");
        viewState.setSortDirection("asc");
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
        }).catch(console.error);
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
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <div className="flex items-center gap-1.5">
          <SidebarTrigger className="md:hidden" />
          <WorkspaceAvatar name={workspace?.name ?? "W"} size="sm" className="hidden md:flex" />
          <span className="text-sm text-muted-foreground hidden md:inline">
            {workspace?.name ?? "Workspace"}
          </span>
          <ChevronRight className="h-3 w-3 text-muted-foreground hidden md:inline" />
          <span className="text-sm font-medium">Issues</span>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          className="md:hidden text-muted-foreground"
          onClick={() => useModalStore.getState().open("create-issue")}
        >
          <Plus className="h-4 w-4" />
        </Button>
      </div>

      {/* Header 2: Scope tabs + filters */}
      <IssuesHeader scopedIssues={scopedIssues} />

      {/* Content: scrollable */}
      <ViewStoreProvider store={useIssueViewStore}>
        {scopedIssues.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">No issues yet</p>
            <p className="text-xs">Create an issue to get started.</p>
          </div>
        ) : (
          <div className="flex flex-col flex-1 min-h-0">
            {viewMode === "board" ? (
              <BoardView
                issues={issues}
                allIssues={scopedIssues}
                visibleStatuses={visibleStatuses}
                hiddenStatuses={hiddenStatuses}
                onMoveIssue={handleMoveIssue}
              />
            ) : (
              <ListView issues={issues} visibleStatuses={visibleStatuses} />
            )}
          </div>
        )}
        {viewMode === "list" && <BatchActionToolbar />}
      </ViewStoreProvider>
    </div>
  );
}
