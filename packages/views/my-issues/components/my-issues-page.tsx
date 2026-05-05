"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useStore } from "zustand";
import { toast } from "sonner";
import { ChevronRight, ListTodo } from "lucide-react";
import type { IssueStatus } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace } from "@multica/core/paths";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { useQuery } from "@tanstack/react-query";
import { agentListOptions } from "@multica/core/workspace/queries";
import { filterIssues } from "../../issues/utils/filter";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { BoardView } from "../../issues/components/board-view";
import { ListView } from "../../issues/components/list-view";
import { BatchActionToolbar } from "../../issues/components/batch-action-toolbar";
import { useClearFiltersOnWorkspaceChange } from "@multica/core/issues/stores/view-store";
import { useWorkspaceId } from "@multica/core/hooks";
import { myIssueListOptions, childIssueProgressOptions, type MyIssuesFilter } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { myIssuesViewStore } from "@multica/core/issues/stores/my-issues-view-store";
import { useT } from "@multica/i18n/react";
import { PageHeader } from "../../layout/page-header";
import { MyIssuesHeader } from "./my-issues-header";

export function MyIssuesPage() {
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const t = useT("issues");

  const viewMode = useStore(myIssuesViewStore, (s) => s.viewMode);
  const statusFilters = useStore(myIssuesViewStore, (s) => s.statusFilters);
  const priorityFilters = useStore(myIssuesViewStore, (s) => s.priorityFilters);
  const scope = useStore(myIssuesViewStore, (s) => s.scope);

  // Clear filter state when switching between workspaces (URL-driven).
  useClearFiltersOnWorkspaceChange(myIssuesViewStore, wsId);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode, scope]);

  // Build server-side filter based on scope
  const myAgentIds = useMemo(() => {
    if (!user) return [] as string[];
    return agents
      .filter((a) => a.owner_id === user.id)
      .map((a) => a.id)
      .sort();
  }, [agents, user]);

  const filter: MyIssuesFilter = useMemo(() => {
    if (!user) return {};
    switch (scope) {
      case "assigned":
        return { assignee_id: user.id };
      case "created":
        return { creator_id: user.id };
      case "agents":
        return { assignee_ids: myAgentIds };
      default:
        return { assignee_id: user.id };
    }
  }, [scope, user, myAgentIds]);

  const { data: myIssues = [], isLoading: loading } = useQuery(
    myIssueListOptions(wsId, scope, filter),
  );

  // Apply status/priority filters from view store
  const issues = useMemo(
    () =>
      filterIssues(myIssues, {
        statusFilters,
        priorityFilters,
        assigneeFilters: [],
        includeNoAssignee: false,
        creatorFilters: [],
        projectFilters: [],
        includeNoProject: false,
        labelFilters: [],
      }),
    [myIssues, statusFilters, priorityFilters],
  );

  const { data: childProgressMap = new Map() } = useQuery(childIssueProgressOptions(wsId));

  const visibleStatuses = useMemo(() => {
    if (statusFilters.length > 0)
      return BOARD_STATUSES.filter((s) => statusFilters.includes(s));
    return BOARD_STATUSES;
  }, [statusFilters]);

  const hiddenStatuses = useMemo(() => {
    return BOARD_STATUSES.filter((s) => !visibleStatuses.includes(s));
  }, [visibleStatuses]);

  const updateIssueMutation = useUpdateIssue();
  const handleMoveIssue = useCallback(
    (issueId: string, newStatus: IssueStatus, newPosition?: number) => {
      const updates: Partial<{ status: IssueStatus; position: number }> = {
        status: newStatus,
      };
      if (newPosition !== undefined) updates.position = newPosition;

      updateIssueMutation.mutate(
        { id: issueId, ...updates },
        { onError: () => toast.error(t("toast_failed_move_issue")) },
      );
    },
    [updateIssueMutation],
  );

  if (loading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="flex h-12 shrink-0 items-center justify-between px-4">
          <div className="flex items-center gap-1">
            <Skeleton className="h-8 w-14 rounded-md" />
            <Skeleton className="h-8 w-20 rounded-md" />
            <Skeleton className="h-8 w-16 rounded-md" />
          </div>
          <div className="flex items-center gap-1">
            <Skeleton className="h-8 w-8 rounded-md" />
            <Skeleton className="h-8 w-8 rounded-md" />
            <Skeleton className="h-8 w-8 rounded-md" />
          </div>
        </div>
        {viewMode === "list" ? (
          <div className="flex-1 min-h-0 overflow-y-auto p-2 space-y-1">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full rounded-lg" />
            ))}
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex min-w-52 flex-1 flex-col gap-2">
                <Skeleton className="h-4 w-20" />
                <Skeleton className="h-24 w-full rounded-lg" />
                <Skeleton className="h-24 w-full rounded-lg" />
              </div>
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Header 1: Workspace breadcrumb */}
      <PageHeader className="gap-1.5">
        <WorkspaceAvatar name={workspace?.name ?? "W"} size="sm" />
        <span className="text-sm text-muted-foreground">
          {workspace?.name ?? "Workspace"}
        </span>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="text-sm font-medium">{t("my_issues_title")}</span>
      </PageHeader>

      {/* Header: scope tabs (left) + controls (right) */}
      <MyIssuesHeader allIssues={myIssues} />

      {/* Content: scrollable */}
      <ViewStoreProvider store={myIssuesViewStore}>
        {myIssues.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t("no_issues_assigned")}</p>
            <p className="text-xs">{t("no_issues_hint")}</p>
          </div>
        ) : (
          <div className="flex flex-col flex-1 min-h-0">
            {viewMode === "board" ? (
              <BoardView
                issues={issues}
                visibleStatuses={visibleStatuses}
                hiddenStatuses={hiddenStatuses}
                onMoveIssue={handleMoveIssue}
                childProgressMap={childProgressMap}
                myIssuesScope={scope}
                myIssuesFilter={filter}
              />
            ) : (
              <ListView
                issues={issues}
                visibleStatuses={visibleStatuses}
                childProgressMap={childProgressMap}
                myIssuesScope={scope}
                myIssuesFilter={filter}
              />
            )}
          </div>
        )}
        {viewMode === "list" && <BatchActionToolbar />}
      </ViewStoreProvider>
    </div>
  );
}
