"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useStore } from "zustand";
import { toast } from "sonner";
import { ChevronRight, ListTodo } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useCurrentWorkspace } from "@multica/core/paths";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { useQuery } from "@tanstack/react-query";
import { filterIssues, EMPTY_CLIENT_FILTERS } from "../../issues/utils/filter";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { BoardView } from "../../issues/components/board-view";
import { ListView } from "../../issues/components/list-view";
import { SwimLaneView } from "../../issues/components/swimlane-view";
import { BatchActionToolbar } from "../../issues/components/batch-action-toolbar";
import { useClearFiltersOnWorkspaceChange, buildActiveViewFilters } from "@multica/core/issues/stores/view-store";
import { viewListOptions } from "@multica/core/views";
import { useWorkspaceId } from "@multica/core/hooks";
import { viewIssueAssigneeGroupsOptions, viewIssueListOptions, viewFiltersToGroupedFilter, withBoardStatusScope, childIssueProgressOptions } from "@multica/core/issues/queries";
import type { SavedView, ViewFilters } from "@multica/core/types";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { myIssuesViewStore } from "@multica/core/issues/stores/my-issues-view-store";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { MyIssuesHeader } from "./my-issues-header";

export function MyIssuesPage() {
  const { t } = useT("my-issues");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const viewMode = useStore(myIssuesViewStore, (s) => s.viewMode);
  const currentViewId = useStore(myIssuesViewStore, (s) => s.currentViewId);
  const setActiveView = useStore(myIssuesViewStore, (s) => s.setActiveView);
  const statusFilters = useStore(myIssuesViewStore, (s) => s.statusFilters);
  const priorityFilters = useStore(myIssuesViewStore, (s) => s.priorityFilters);
  const assigneeFilters = useStore(myIssuesViewStore, (s) => s.assigneeFilters);
  const includeNoAssignee = useStore(myIssuesViewStore, (s) => s.includeNoAssignee);
  const creatorFilters = useStore(myIssuesViewStore, (s) => s.creatorFilters);
  const projectFilters = useStore(myIssuesViewStore, (s) => s.projectFilters);
  const includeNoProject = useStore(myIssuesViewStore, (s) => s.includeNoProject);
  const labelFilters = useStore(myIssuesViewStore, (s) => s.labelFilters);
  const grouping = useStore(myIssuesViewStore, (s) => s.grouping);
  const sortBy = useStore(myIssuesViewStore, (s) => s.sortBy);
  const sortDirection = useStore(myIssuesViewStore, (s) => s.sortDirection);
  const agentRunningFilter = useStore(myIssuesViewStore, (s) => s.agentRunningFilter);
  const usesAssigneeBoard = viewMode === "board" && grouping === "assignee";

  const sort = useMemo(
    () => ({
      sort_by: sortBy,
      sort_direction: sortBy !== "position" ? sortDirection : undefined,
    } as const),
    [sortBy, sortDirection],
  );

  // See issues-page.tsx for the rationale — derive a workspace-wide set
  // of issue ids with at least one running task, drive the "agents
  // working" quick-filter from it.
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const runningIssueIds = useMemo(() => {
    const ids = new Set<string>();
    for (const t of snapshot) {
      if (t.status === "running" && t.issue_id) ids.add(t.issue_id);
    }
    return ids;
  }, [snapshot]);

  // Clear filter state when switching between workspaces (URL-driven).
  useClearFiltersOnWorkspaceChange(myIssuesViewStore, wsId);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode, currentViewId]);

  // View-driven fetch — same model as IssuesPage. The {me}/{my_agents}/
  // {my_squads} tokens inside the saved view's filters are expanded by the
  // server, so the page no longer needs the user's id or per-scope branching.
  const { data: savedViews } = useQuery(viewListOptions(wsId, "my_issues"));
  const activeView = useMemo<SavedView | undefined>(
    () => savedViews?.find((v) => v.id === currentViewId),
    [savedViews, currentViewId],
  );
  const activeFilters = useMemo<ViewFilters>(
    () =>
      buildActiveViewFilters(
        {
          statusFilters,
          priorityFilters,
          assigneeFilters,
          includeNoAssignee,
          creatorFilters,
          projectFilters,
          includeNoProject,
          labelFilters,
        },
        activeView,
      ),
    [activeView, assigneeFilters, creatorFilters, includeNoAssignee, includeNoProject, labelFilters, priorityFilters, projectFilters, statusFilters],
  );

  const loadMoreView = useMemo(
    () => ({ viewId: currentViewId, filters: activeFilters }),
    [currentViewId, activeFilters],
  );

  // Grouped (assignee board) fetch defaults to BOARD_STATUSES when no explicit
  // status filter is set — see withBoardStatusScope.
  const boardFilters = useMemo<ViewFilters>(
    () => withBoardStatusScope(activeFilters),
    [activeFilters],
  );

  const assigneeGroupFilter = useMemo(
    () => (boardFilters.any_of ? undefined : viewFiltersToGroupedFilter(boardFilters)),
    [boardFilters],
  );

  const assigneeGroupsOptions = viewIssueAssigneeGroupsOptions(wsId, currentViewId, boardFilters, sort);
  const statusIssuesQuery = useQuery({
    ...viewIssueListOptions(wsId, currentViewId, activeFilters, sort),
    enabled: !usesAssigneeBoard,
  });
  const assigneeGroupsQuery = useQuery({
    ...assigneeGroupsOptions,
    enabled: usesAssigneeBoard,
  });
  const myIssues = useMemo(
    () =>
      usesAssigneeBoard
        ? (assigneeGroupsQuery.data?.groups.flatMap((group) => group.issues) ?? [])
        : (statusIssuesQuery.data ?? []),
    [assigneeGroupsQuery.data, statusIssuesQuery.data, usesAssigneeBoard],
  );
  const loading = usesAssigneeBoard
    ? assigneeGroupsQuery.isLoading
    : statusIssuesQuery.isLoading;

  // The view + filter bar filter server-side now; the only remaining client
  // filter is the ephemeral `agentRunningFilter` chip (see IssuesPage).
  const issues = useMemo(
    () => filterIssues(myIssues, { ...EMPTY_CLIENT_FILTERS, agentRunningFilter, runningIssueIds }),
    [myIssues, agentRunningFilter, runningIssueIds],
  );

  // Status-unfiltered companion for Swimlane.
  const swimlaneIssues = useMemo(
    () => filterIssues(myIssues, { ...EMPTY_CLIENT_FILTERS, agentRunningFilter, runningIssueIds }),
    [myIssues, agentRunningFilter, runningIssueIds],
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
    (issueId: string, updates: Pick<UpdateIssueRequest, "status" | "assignee_type" | "assignee_id" | "position" | "parent_issue_id">, onSettled?: () => void) => {
      updateIssueMutation.mutate(
        { id: issueId, ...updates },
        {
          onError: (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.errors.move_failed),
            ),
          onSettled: () => onSettled?.(),
        },
      );
    },
    [updateIssueMutation, t],
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
          <div className="flex-1 min-h-0 overflow-y-auto p-2 pt-0 space-y-1">
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
          {workspace?.name ?? t(($) => $.page.workspace_fallback)}
        </span>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="text-sm font-medium">{t(($) => $.page.breadcrumb)}</span>
      </PageHeader>

      <ViewStoreProvider store={myIssuesViewStore}>
        {/* Header: saved-view tabs (left) + controls (right) */}
        <MyIssuesHeader
          allIssues={myIssues}
          currentViewId={currentViewId}
          onSelectView={setActiveView}
        />
        {myIssues.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.page.empty_title)}</p>
            <p className="text-xs">{t(($) => $.page.empty_description)}</p>
          </div>
        ) : (
          <div className="flex flex-col flex-1 min-h-0">
            {viewMode === "board" ? (
              <BoardView
                issues={usesAssigneeBoard ? myIssues : issues}
                assigneeGroups={usesAssigneeBoard ? assigneeGroupsQuery.data?.groups : undefined}
                assigneeGroupQueryKey={usesAssigneeBoard ? assigneeGroupsOptions.queryKey : undefined}
                assigneeGroupFilter={usesAssigneeBoard ? assigneeGroupFilter : undefined}
                visibleStatuses={visibleStatuses}
                hiddenStatuses={hiddenStatuses}
                onMoveIssue={handleMoveIssue}
                childProgressMap={childProgressMap}
                view={loadMoreView}
                sort={sort}
              />
            ) : viewMode === "swimlane" ? (
              <SwimLaneView
                issues={issues}
                unfilteredIssues={swimlaneIssues}
                visibleStatuses={visibleStatuses}
                hiddenStatuses={hiddenStatuses}
                onMoveIssue={handleMoveIssue}
                childProgressMap={childProgressMap}
                view={loadMoreView}
                sort={sort}
              />
            ) : (
              <ListView
                issues={issues}
                visibleStatuses={visibleStatuses}
                childProgressMap={childProgressMap}
                view={loadMoreView}
                sort={sort}
                onMoveIssue={handleMoveIssue}
              />
            )}
          </div>
        )}
        {viewMode === "list" && <BatchActionToolbar />}
      </ViewStoreProvider>
    </div>
  );
}
