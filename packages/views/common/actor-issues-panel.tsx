"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useStore } from "zustand";
import { toast } from "sonner";
import { ListTodo } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { UpdateIssueRequest } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import {
  childIssueProgressOptions,
  myIssueAssigneeGroupsOptions,
  myIssueListOptions,
  type AssigneeGroupedIssuesFilter,
  type MyIssuesFilter,
} from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import {
  actorIssuesViewStore,
  type ActorIssuesScope,
} from "@multica/core/issues/stores/actor-issues-view-store";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { useClearFiltersOnWorkspaceChange } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { BoardView } from "../issues/components/board-view";
import { ListView } from "../issues/components/list-view";
import { BatchActionToolbar } from "../issues/components/batch-action-toolbar";
import { IssueDisplayControls } from "../issues/components/issues-header";
import { filterIssues } from "../issues/utils/filter";
import { useT } from "../i18n";

export type TaskActorType = "member" | "agent";

const SCOPE_VALUES: ActorIssuesScope[] = ["assigned", "created"];

export function ActorIssuesPanel({
  actorType,
  actorId,
}: {
  actorType: TaskActorType;
  actorId: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const scope = useStore(actorIssuesViewStore, (s) => s.scope);
  const setScope = useStore(actorIssuesViewStore, (s) => s.setScope);
  const viewMode = useStore(actorIssuesViewStore, (s) => s.viewMode);
  const grouping = useStore(actorIssuesViewStore, (s) => s.grouping);
  const statusFilters = useStore(actorIssuesViewStore, (s) => s.statusFilters);
  const priorityFilters = useStore(actorIssuesViewStore, (s) => s.priorityFilters);
  const assigneeFilters = useStore(actorIssuesViewStore, (s) => s.assigneeFilters);
  const includeNoAssignee = useStore(actorIssuesViewStore, (s) => s.includeNoAssignee);
  const creatorFilters = useStore(actorIssuesViewStore, (s) => s.creatorFilters);
  const projectFilters = useStore(actorIssuesViewStore, (s) => s.projectFilters);
  const includeNoProject = useStore(actorIssuesViewStore, (s) => s.includeNoProject);
  const labelFilters = useStore(actorIssuesViewStore, (s) => s.labelFilters);

  useClearFiltersOnWorkspaceChange(actorIssuesViewStore, wsId);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode, scope, actorType, actorId]);

  const queryFilter: MyIssuesFilter = useMemo(
    () =>
      scope === "assigned"
        ? { assignee_id: actorId }
        : { creator_id: actorId },
    [scope, actorId],
  );
  const queryScope = `${actorType}:${actorId}:${scope}`;
  const usesAssigneeBoard = viewMode === "board" && grouping === "assignee";

  const assigneeGroupFilter = useMemo<AssigneeGroupedIssuesFilter>(() => {
    const filter: AssigneeGroupedIssuesFilter = {
      ...queryFilter,
      statuses: statusFilters.length > 0 ? statusFilters : [...BOARD_STATUSES],
      priorities: priorityFilters,
      assignee_filters: assigneeFilters,
      include_no_assignee: includeNoAssignee,
      creator_filters: creatorFilters,
      project_ids: projectFilters,
      include_no_project: includeNoProject,
      label_ids: labelFilters,
    };
    if (scope === "assigned") {
      filter.assignee_types = [actorType];
    }
    return filter;
  }, [
    actorType,
    assigneeFilters,
    creatorFilters,
    includeNoAssignee,
    includeNoProject,
    labelFilters,
    priorityFilters,
    projectFilters,
    queryFilter,
    scope,
    statusFilters,
  ]);
  const assigneeGroupsOptions = myIssueAssigneeGroupsOptions(
    wsId,
    queryScope,
    assigneeGroupFilter,
  );
  const rawIssuesQuery = useQuery({
    ...myIssueListOptions(wsId, queryScope, queryFilter),
    enabled: !usesAssigneeBoard,
  });
  const assigneeGroupsQuery = useQuery({
    ...assigneeGroupsOptions,
    enabled: usesAssigneeBoard,
  });
  const rawIssues = useMemo(
    () => rawIssuesQuery.data ?? [],
    [rawIssuesQuery.data],
  );
  const groupedIssues = useMemo(
    () => assigneeGroupsQuery.data?.groups.flatMap((group) => group.issues) ?? [],
    [assigneeGroupsQuery.data],
  );
  const isLoading = usesAssigneeBoard
    ? assigneeGroupsQuery.isLoading
    : rawIssuesQuery.isLoading;

  const actorIssues = useMemo(
    () =>
      (usesAssigneeBoard ? groupedIssues : rawIssues).filter((issue) =>
        scope === "assigned"
          ? issue.assignee_type === actorType && issue.assignee_id === actorId
          : issue.creator_type === actorType && issue.creator_id === actorId,
      ),
    [actorId, actorType, groupedIssues, rawIssues, scope, usesAssigneeBoard],
  );

  const issues = useMemo(
    () =>
      filterIssues(actorIssues, {
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
        projectFilters,
        includeNoProject,
        labelFilters,
      }),
    [
      actorIssues,
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
    ],
  );

  const { data: childProgressMap = new Map() } = useQuery(
    childIssueProgressOptions(wsId),
  );

  const visibleStatuses = useMemo(() => {
    if (statusFilters.length > 0) {
      return BOARD_STATUSES.filter((s) => statusFilters.includes(s));
    }
    return BOARD_STATUSES;
  }, [statusFilters]);

  const hiddenStatuses = useMemo(
    () => BOARD_STATUSES.filter((s) => !visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const updateIssueMutation = useUpdateIssue();
  const handleMoveIssue = useCallback(
    (issueId: string, updates: Pick<UpdateIssueRequest, "status" | "assignee_type" | "assignee_id" | "position">) => {
      updateIssueMutation.mutate(
        { id: issueId, ...updates },
        { onError: () => toast.error(t(($) => $.page.move_failed)) },
      );
    },
    [updateIssueMutation, t],
  );

  if (isLoading) {
    return <ActorIssuesSkeleton />;
  }

  return (
    <ViewStoreProvider store={actorIssuesViewStore}>
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
          <div className="flex items-center gap-1">
            {SCOPE_VALUES.map((value) => (
              <Tooltip key={value}>
                <TooltipTrigger
                  render={
                    <Button
                      variant="outline"
                      size="sm"
                      className={
                        scope === value
                          ? "bg-accent text-accent-foreground hover:bg-accent/80"
                          : "text-muted-foreground"
                      }
                      onClick={() => setScope(value)}
                    >
                      {t(($) => $.actor_issues.scope[value].label)}
                    </Button>
                  }
                />
                <TooltipContent side="bottom">
                  {t(($) => $.actor_issues.scope[value].description)}
                </TooltipContent>
              </Tooltip>
            ))}
          </div>
          <IssueDisplayControls scopedIssues={actorIssues} />
        </div>

        {actorIssues.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">
              {t(($) => $.actor_issues.empty[scope].title)}
            </p>
            <p className="text-xs">
              {t(($) => $.actor_issues.empty[scope].description)}
            </p>
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 flex-col">
            {viewMode === "board" ? (
              <BoardView
                issues={usesAssigneeBoard ? actorIssues : issues}
                assigneeGroups={usesAssigneeBoard ? assigneeGroupsQuery.data?.groups : undefined}
                assigneeGroupQueryKey={usesAssigneeBoard ? assigneeGroupsOptions.queryKey : undefined}
                assigneeGroupFilter={usesAssigneeBoard ? assigneeGroupFilter : undefined}
                visibleStatuses={visibleStatuses}
                hiddenStatuses={hiddenStatuses}
                onMoveIssue={handleMoveIssue}
                childProgressMap={childProgressMap}
                myIssuesScope={queryScope}
                myIssuesFilter={queryFilter}
              />
            ) : (
              <ListView
                issues={issues}
                visibleStatuses={visibleStatuses}
                childProgressMap={childProgressMap}
                myIssuesScope={queryScope}
                myIssuesFilter={queryFilter}
              />
            )}
          </div>
        )}
        {viewMode === "list" && <BatchActionToolbar />}
      </div>
    </ViewStoreProvider>
  );
}

function ActorIssuesSkeleton() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <div className="flex items-center gap-1">
          <Skeleton className="h-8 w-20 rounded-md" />
          <Skeleton className="h-8 w-20 rounded-md" />
        </div>
        <div className="flex items-center gap-1">
          <Skeleton className="h-8 w-8 rounded-md" />
          <Skeleton className="h-8 w-8 rounded-md" />
          <Skeleton className="h-8 w-8 rounded-md" />
        </div>
      </div>
      <div className="flex flex-1 min-h-0 gap-4 overflow-hidden p-4">
        {Array.from({ length: 4 }).map((_, i) => (
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
