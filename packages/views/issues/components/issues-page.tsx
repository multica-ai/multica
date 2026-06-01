"use client";

import { useCallback, useEffect, useMemo } from "react";
import { toast } from "sonner";
import { ListTodo } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useQuery } from "@tanstack/react-query";
import { useIssueViewStore, useClearFiltersOnWorkspaceChange, buildActiveViewFilters } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { viewListOptions } from "@multica/core/views";
import { filterIssues, EMPTY_CLIENT_FILTERS } from "../utils/filter";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { viewIssueAssigneeGroupsOptions, viewIssueListOptions, viewFiltersToGroupedFilter, withBoardStatusScope, childIssueProgressOptions } from "@multica/core/issues/queries";
import type { SavedView, ViewFilters } from "@multica/core/types";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { PageHeader } from "../../layout/page-header";
import { IssuesHeader } from "./issues-header";
import { BoardView } from "./board-view";
import { ListView } from "./list-view";
import { SwimLaneView } from "./swimlane-view";
import { BatchActionToolbar } from "./batch-action-toolbar";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";

const EMPTY_CHILD_PROGRESS = new Map<string, ChildProgress>();

export function IssuesPage() {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();

  const currentViewId = useIssueViewStore((s) => s.currentViewId);
  const setActiveView = useIssueViewStore((s) => s.setActiveView);
  const viewMode = useIssueViewStore((s) => s.viewMode);
  const grouping = useIssueViewStore((s) => s.grouping);
  const statusFilters = useIssueViewStore((s) => s.statusFilters);
  const priorityFilters = useIssueViewStore((s) => s.priorityFilters);
  const assigneeFilters = useIssueViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useIssueViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useIssueViewStore((s) => s.creatorFilters);
  const projectFilters = useIssueViewStore((s) => s.projectFilters);
  const includeNoProject = useIssueViewStore((s) => s.includeNoProject);
  const labelFilters = useIssueViewStore((s) => s.labelFilters);
  const sortBy = useIssueViewStore((s) => s.sortBy);
  const sortDirection = useIssueViewStore((s) => s.sortDirection);
  const agentRunningFilter = useIssueViewStore((s) => s.agentRunningFilter);
  const usesAssigneeBoard = viewMode === "board" && grouping === "assignee";

  const sort = useMemo(
    () => ({
      sort_by: sortBy,
      sort_direction: sortBy !== "position" ? sortDirection : undefined,
    } as const),
    [sortBy, sortDirection],
  );

  // Derive the set of issue ids that currently have at least one
  // `running` agent task. Used by the workspace agents-working filter
  // chip. Subscribing the page here (not deep in filter.ts) keeps the
  // filter pure and lets the snapshot stay cached at one workspace-
  // scoped place — every issue card already subscribes for its own
  // indicator, so this is a no-op extra fetch.
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const runningIssueIds = useMemo(() => {
    const ids = new Set<string>();
    for (const t of snapshot) {
      if (t.status === "running" && t.issue_id) ids.add(t.issue_id);
    }
    return ids;
  }, [snapshot]);

  // The active view's RAW stored filters (with any_of intact) are the source
  // of truth for the server fetch. For a flat view, the filter-bar edits are
  // already mirrored into the store fields (setActiveView loads them), so we
  // reconstruct from the store. An any_of view can't be reconstructed flat
  // (the filter bar has no OR), so we fetch from its raw filters and the
  // filter-bar narrowing is layered as outer AND fields onto every branch.
  const { data: savedViews } = useQuery(viewListOptions(wsId, "issues"));
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

  // The assignee-grouped board issues a single grouped fetch (not a per-status
  // sweep), so it must default to BOARD_STATUSES when no status filter is set —
  // see withBoardStatusScope. The per-status list path keeps using activeFilters.
  const boardFilters = useMemo<ViewFilters>(
    () => withBoardStatusScope(activeFilters),
    [activeFilters],
  );

  // The assignee-grouped board's per-group load-more needs the flat grouped
  // filter. Only flat (non-any_of) views can paginate a group — for an any_of
  // view this is undefined, which makes BoardView render plain (no-load-more)
  // assignee columns. See viewIssueAssigneeGroupsOptions / the plan's note that
  // multi-branch pagination is intentionally disabled.
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
  const allIssues = useMemo(
    () => statusIssuesQuery.data ?? [],
    [statusIssuesQuery.data],
  );
  const assigneeIssues = useMemo(
    () => assigneeGroupsQuery.data?.groups.flatMap((group) => group.issues) ?? [],
    [assigneeGroupsQuery.data],
  );
  const loading = usesAssigneeBoard
    ? assigneeGroupsQuery.isLoading
    : statusIssuesQuery.isLoading;

  // Clear filter state when switching between workspaces (URL-driven).
  useClearFiltersOnWorkspaceChange(useIssueViewStore, wsId);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode, currentViewId]);

  const headerIssues = usesAssigneeBoard ? assigneeIssues : allIssues;

  // The view + filter bar now filter server-side, so the only remaining
  // client filter is the ephemeral `agentRunningFilter` chip (never persisted,
  // not expressible as a saved-view param). Everything else is already applied
  // by viewIssueListOptions.
  const issues = useMemo(
    () => filterIssues(allIssues, { ...EMPTY_CLIENT_FILTERS, agentRunningFilter, runningIssueIds }),
    [allIssues, agentRunningFilter, runningIssueIds],
  );

  // Status-unfiltered companion for Swimlane. Status narrowing happens
  // server-side via the view's `statuses`; the swimlane still wants every
  // returned issue regardless of the board's collapsed columns, which the
  // visibleStatuses split below handles — so this mirrors `issues`.
  const swimlaneIssues = useMemo(
    () => filterIssues(allIssues, { ...EMPTY_CLIENT_FILTERS, agentRunningFilter, runningIssueIds }),
    [allIssues, agentRunningFilter, runningIssueIds],
  );

  // Fetch sub-issue progress from the backend so counts are accurate
  // regardless of client-side pagination or filtering of done issues.
  const { data: childProgressMap = EMPTY_CHILD_PROGRESS } = useQuery(childIssueProgressOptions(wsId));

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
                : t(($) => $.page.move_failed),
            ),
          onSettled: () => onSettled?.(),
        },
      );
    },
    [updateIssueMutation, t],
  );

  const contentSkeleton = viewMode === "list" ? (
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
  );

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.breadcrumb_title)}</h1>
      </PageHeader>

      <ViewStoreProvider store={useIssueViewStore}>
        <IssuesHeader
          scopedIssues={headerIssues}
          viewTabs={{
            page: "issues",
            currentViewId,
            onSelectView: setActiveView,
            currentFilters: activeFilters,
            resolveDefaultName: (nameKey) =>
              t(($) => $.views.defaults[nameKey as keyof typeof $.views.defaults] ?? nameKey),
          }}
        />

        {loading ? contentSkeleton : headerIssues.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.page.empty_title)}</p>
            <p className="text-xs">{t(($) => $.page.empty_hint)}</p>
          </div>
        ) : (
          <div className="flex flex-col flex-1 min-h-0">
            {viewMode === "board" ? (
              <BoardView
                issues={usesAssigneeBoard ? assigneeIssues : issues}
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
              <ListView issues={issues} visibleStatuses={visibleStatuses} childProgressMap={childProgressMap} view={loadMoreView} sort={sort} onMoveIssue={handleMoveIssue} />
            )}
          </div>
        )}
        {viewMode === "list" && <BatchActionToolbar />}
      </ViewStoreProvider>
    </div>
  );
}
