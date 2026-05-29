"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useStore } from "zustand";
import { toast } from "sonner";
import { ListTodo } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useAuthStore } from "@multica/core/auth";
import { useQuery } from "@tanstack/react-query";
import { filterIssues } from "../../issues/utils/filter";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { BoardView } from "../../issues/components/board-view";
import { ListView } from "../../issues/components/list-view";
import { SwimLaneView } from "../../issues/components/swimlane-view";
import { BatchActionToolbar } from "../../issues/components/batch-action-toolbar";
import { useClearFiltersOnWorkspaceChange } from "@multica/core/issues/stores/view-store";
import { useWorkspaceId } from "@multica/core/hooks";
import { myIssueAssigneeGroupsOptions, myIssueListOptions, childIssueProgressOptions, type AssigneeGroupedIssuesFilter, type MyIssuesFilter } from "@multica/core/issues/queries";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { myIssuesViewStore } from "@multica/core/issues/stores/my-issues-view-store";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { MyIssuesHeader } from "./my-issues-header";

export function MyIssuesPage() {
  const { t } = useT("my-issues");
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const viewMode = useStore(myIssuesViewStore, (s) => s.viewMode);
  const statusFilters = useStore(myIssuesViewStore, (s) => s.statusFilters);
  const priorityFilters = useStore(myIssuesViewStore, (s) => s.priorityFilters);
  const scope = useStore(myIssuesViewStore, (s) => s.scope);
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
  }, [viewMode, scope]);

  // Build server-side filter based on scope. The `agents` tab uses
  // `involves_user_id` so the server expands the user's identity to all
  // assignees that indirectly belong to them (owned agents + related squads).
  // Direct member assignment is intentionally excluded — that is the
  // `assigned` tab's semantics.
  const filter: MyIssuesFilter = useMemo(() => {
    if (!user) return {};
    switch (scope) {
      case "assigned":
        return { assignee_id: user.id };
      case "created":
        return { creator_id: user.id };
      case "agents":
        return { involves_user_id: user.id };
      case "all":
        // "All" is the union of the three single-relation filters above;
        // the per-relation user id is plumbed through `userId` to
        // myIssue*Options. The filter object stays empty so it carries
        // no narrowing of its own.
        return {};
      default:
        return { assignee_id: user.id };
    }
  }, [scope, user]);

  const assigneeGroupFilter = useMemo<AssigneeGroupedIssuesFilter>(
    () => ({
      ...filter,
      statuses: statusFilters.length > 0 ? statusFilters : [...BOARD_STATUSES],
      priorities: priorityFilters,
    }),
    [filter, priorityFilters, statusFilters],
  );
  const assigneeGroupsOptions = myIssueAssigneeGroupsOptions(
    wsId,
    scope,
    assigneeGroupFilter,
    user?.id,
    sort,
  );
  const statusIssuesQuery = useQuery({
    ...myIssueListOptions(wsId, scope, filter, user?.id, sort),
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

  // Apply status/priority/agent-running filters from view store
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
        agentRunningFilter,
        runningIssueIds,
      }),
    [myIssues, statusFilters, priorityFilters, agentRunningFilter, runningIssueIds],
  );

  // Status-unfiltered companion for Swimlane.
  const swimlaneIssues = useMemo(
    () =>
      filterIssues(myIssues, {
        statusFilters: [],
        priorityFilters,
        assigneeFilters: [],
        includeNoAssignee: false,
        creatorFilters: [],
        projectFilters: [],
        includeNoProject: false,
        labelFilters: [],
        agentRunningFilter,
        runningIssueIds,
      }),
    [myIssues, priorityFilters, agentRunningFilter, runningIssueIds],
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
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.breadcrumb)}</h1>
      </PageHeader>

      <ViewStoreProvider store={myIssuesViewStore}>
        {/* Header: scope tabs (left) + controls (right) */}
        <MyIssuesHeader allIssues={myIssues} />
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
                myIssuesScope={scope}
                myIssuesFilter={filter}
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
                myIssuesScope={scope}
                myIssuesFilter={filter}
                sort={sort}
              />
            ) : (
              <ListView
                issues={issues}
                visibleStatuses={visibleStatuses}
                childProgressMap={childProgressMap}
                myIssuesScope={scope}
                myIssuesFilter={filter}
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
