"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useStore } from "zustand";
import { toast } from "sonner";
import { ChevronRight, ListTodo } from "lucide-react";
import type { IssueStatus } from "@/shared/types";
import { Skeleton } from "@/components/ui/skeleton";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore, WorkspaceAvatar } from "@/features/workspace";
import { useIssueStore } from "@/features/issues/store";
import { filterIssues } from "@/features/issues/utils/filter";
import { BOARD_STATUSES } from "@/features/issues/config";
import { ViewStoreProvider } from "@/features/issues/stores/view-store-context";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { BoardView } from "@/features/issues/components/board-view";
import { ListView } from "@/features/issues/components/list-view";
import { BatchActionToolbar } from "@/features/issues/components/batch-action-toolbar";
import { registerViewStoreForWorkspaceSync } from "@/features/issues/stores/view-store";
import { api } from "@/shared/api";
import { myIssuesViewStore } from "../stores/my-issues-view-store";
import { MyIssuesHeader } from "./my-issues-header";

export function MyIssuesPage() {
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const agents = useWorkspaceStore((s) => s.agents);
  const allIssues = useIssueStore((s) => s.issues);
  const loading = useIssueStore((s) => s.loading);

  const viewMode = useStore(myIssuesViewStore, (s) => s.viewMode);
  const statusFilters = useStore(myIssuesViewStore, (s) => s.statusFilters);
  const priorityFilters = useStore(myIssuesViewStore, (s) => s.priorityFilters);
  const scope = useStore(myIssuesViewStore, (s) => s.scope);

  useEffect(() => {
    registerViewStoreForWorkspaceSync(myIssuesViewStore);
  }, []);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode, scope]);

  const myAgentIds = useMemo(() => {
    if (!user) return new Set<string>();
    return new Set(
      agents.filter((a) => a.owner_id === user.id).map((a) => a.id),
    );
  }, [agents, user]);

  // Per-scope issue lists
  const assignedToMe = useMemo(() => {
    if (!user) return [];
    return allIssues.filter(
      (i) => i.assignee_type === "member" && i.assignee_id === user.id,
    );
  }, [allIssues, user]);

  const myAgentIssues = useMemo(() => {
    if (!user) return [];
    return allIssues.filter(
      (i) =>
        i.assignee_type === "agent" &&
        i.assignee_id &&
        myAgentIds.has(i.assignee_id),
    );
  }, [allIssues, user, myAgentIds]);

  const createdByMe = useMemo(() => {
    if (!user) return [];
    return allIssues.filter(
      (i) => i.creator_type === "member" && i.creator_id === user.id,
    );
  }, [allIssues, user]);

  const myIssues = useMemo(() => {
    switch (scope) {
      case "assigned": return assignedToMe;
      case "agents": return myAgentIssues;
      case "created": return createdByMe;
      default: return assignedToMe;
    }
  }, [scope, assignedToMe, myAgentIssues, createdByMe]);

  // Apply status/priority filters from view store
  const issues = useMemo(
    () =>
      filterIssues(myIssues, {
        statusFilters,
        priorityFilters,
        assigneeFilters: [],
        includeNoAssignee: false,
        creatorFilters: [],
      }),
    [myIssues, statusFilters, priorityFilters],
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
      const viewState = myIssuesViewStore.getState();
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
    [],
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
        <span className="text-sm font-medium">My Issues</span>
      </div>

      {/* Header: scope tabs (left) + controls (right) */}
      <MyIssuesHeader allIssues={myIssues} />

      {/* Content: scrollable */}
      <ViewStoreProvider store={myIssuesViewStore}>
        {myIssues.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">No issues assigned to you</p>
            <p className="text-xs">Issues you create or are assigned to will appear here.</p>
          </div>
        ) : (
          <div className="flex flex-col flex-1 min-h-0">
            {viewMode === "board" ? (
              <BoardView
                issues={issues}
                allIssues={myIssues}
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
