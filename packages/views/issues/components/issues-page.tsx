"use client";

// CEREBRO-PATCH(issues-page-cerebro): cerebro modification of upstream file

import { useCallback, useEffect, useMemo, type ReactNode } from "react";
import { toast } from "sonner";
import { ListTodo } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useQuery } from "@tanstack/react-query";
import { useIssueViewStore, useClearFiltersOnWorkspaceChange } from "@multica/core/issues/stores/view-store";
import { useIssuesScopeStore } from "@multica/core/issues/stores/issues-scope-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { filterIssues } from "../utils/filter";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueAssigneeGroupsOptions, issueListOptions, childIssueProgressOptions, type AssigneeGroupedIssuesFilter } from "@multica/core/issues/queries";
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
import { useNavigation } from "../../navigation";

const EMPTY_CHILD_PROGRESS = new Map<string, ChildProgress>();

// CEREBRO-PATCH(issue-reference-filter): parse `<object>:<ref_id>` edge-reference filter.
function parseReferenceFilter(searchValue: string | null): string | undefined {
  if (!searchValue) return undefined;
  const [object, ...rest] = searchValue.split(":");
  const refId = rest.join(":");
  if (!object || !refId) return undefined;
  return `${object}:${refId}`;
}

export function IssuesPage({
  referenceFilterControl,
}: {
  referenceFilterControl?: ReactNode;
} = {}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();

  const scope = useIssuesScopeStore((s) => s.scope);
  const viewMode = useIssueViewStore((s) => s.viewMode);
  const grouping = useIssueViewStore((s) => s.grouping);
  const statusFilters = useIssueViewStore((s) => s.statusFilters);
  const priorityFilters = useIssueViewStore((s) => s.priorityFilters);
  const assigneeFilters = useIssueViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useIssueViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useIssueViewStore((s) => s.creatorFilters);
  const projectFilters = useIssueViewStore((s) => s.projectFilters);
  const includeNoProject = useIssueViewStore((s) => s.includeNoProject);
  const subIssueDisplay = useIssueViewStore((s) => s.subIssueDisplay);
  const labelFilters = useIssueViewStore((s) => s.labelFilters);
  // CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 on-behalf-of member filter.
  const onBehalfOfFilters = useIssueViewStore((s) => s.onBehalfOfFilters);
  const referenceFilter = useMemo(
    () => parseReferenceFilter(navigation.searchParams.get("reference")),
    [navigation.searchParams],
  );
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

  const assigneeGroupFilter = useMemo<AssigneeGroupedIssuesFilter>(() => {
    const filter: AssigneeGroupedIssuesFilter = {
      statuses: statusFilters.length > 0 ? statusFilters : [...BOARD_STATUSES],
      priorities: priorityFilters,
      assignee_filters: assigneeFilters,
      include_no_assignee: includeNoAssignee,
      creator_filters: creatorFilters,
      project_ids: projectFilters,
      include_no_project: includeNoProject,
      label_ids: labelFilters,
      reference: referenceFilter,
      // CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 board view on-behalf-of filter.
      on_behalf_of_ids: onBehalfOfFilters,
    };
    if (scope === "members") filter.assignee_types = ["member"];
    if (scope === "agents") filter.assignee_types = ["agent", "squad"];
    return filter;
  }, [assigneeFilters, creatorFilters, includeNoAssignee, includeNoProject, labelFilters, onBehalfOfFilters, priorityFilters, projectFilters, referenceFilter, scope, statusFilters]);

  const assigneeGroupsOptions = issueAssigneeGroupsOptions(wsId, assigneeGroupFilter, sort);
  const statusIssuesQuery = useQuery({
    // CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 list view on-behalf-of filter.
    ...issueListOptions(wsId, { reference: referenceFilter, on_behalf_of_ids: onBehalfOfFilters }, sort),
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
  }, [viewMode, scope]);

  // Scope pre-filter: narrow by assignee type
  const scopedIssues = useMemo(() => {
    if (scope === "members")
      return allIssues.filter((i) => i.assignee_type === "member");
    if (scope === "agents")
      return allIssues.filter((i) => i.assignee_type === "agent" || i.assignee_type === "squad");
    return allIssues;
  }, [allIssues, scope]);

  // Sub-issue display filter: hide child issues from the list when not "standalone"
  const displayIssues = useMemo(() => {
    if (subIssueDisplay === "standalone") return scopedIssues;
    // "on-parent" and "hidden" both remove child issues from the top-level list
    return scopedIssues.filter((i) => !i.parent_issue_id);
  }, [scopedIssues, subIssueDisplay]);

  const issues = useMemo(
    () => filterIssues(displayIssues, { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, agentRunningFilter, runningIssueIds }),
    [displayIssues, statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, agentRunningFilter, runningIssueIds],
  );
  const headerIssues = usesAssigneeBoard ? assigneeIssues : issues;

  // Status-unfiltered companion for Swimlane — same narrowing as `issues`
  // minus the status filter.
  const swimlaneIssues = useMemo(
    () => filterIssues(scopedIssues, { statusFilters: [], priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, agentRunningFilter, runningIssueIds }),
    [scopedIssues, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, agentRunningFilter, runningIssueIds],
  );

  // Fetch sub-issue progress from the backend so counts are accurate
  // regardless of client-side pagination or filtering of done issues.
  const { data: serverProgressMap = EMPTY_CHILD_PROGRESS } = useQuery(childIssueProgressOptions(wsId));
  // CEREBRO-PATCH(sub-issue-display): when sub-issues are hidden entirely, suppress progress indicators too.
  const childProgressMap = subIssueDisplay === "hidden" ? EMPTY_CHILD_PROGRESS : serverProgressMap;

  // CEREBRO-PATCH(sub-issue-display): children-by-parent map for inline nesting in "on-parent" mode.
  const childrenMap = useMemo(() => {
    if (subIssueDisplay !== "on-parent") return new Map<string, typeof scopedIssues>();
    const map = new Map<string, typeof scopedIssues>();
    for (const issue of scopedIssues) {
      if (issue.parent_issue_id) {
        const children = map.get(issue.parent_issue_id) ?? [];
        children.push(issue);
        map.set(issue.parent_issue_id, children);
      }
    }
    return map;
  }, [scopedIssues, subIssueDisplay]);

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
        {/* Header 2: Scope tabs + filters */}
        <IssuesHeader
          scopedIssues={headerIssues}
          referenceFilterControl={referenceFilterControl}
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
                sort={sort}
              />
            ) : (
              <ListView issues={issues} visibleStatuses={visibleStatuses} childProgressMap={childProgressMap} childrenMap={childrenMap} sort={sort} onMoveIssue={handleMoveIssue} />
            )}
          </div>
        )}
        {viewMode === "list" && <BatchActionToolbar />}
      </ViewStoreProvider>
    </div>
  );
}
