"use client";

import { useEffect, useMemo, useState } from "react";
import { useStore } from "zustand";
import { ListTodo, Search } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import {
  childIssueProgressOptions,
  myIssueListOptions,
  type MyIssuesFilter,
} from "@multica/core/issues/queries";
import {
  actorIssuesViewStore,
  type ActorIssuesScope,
} from "@multica/core/issues/stores/actor-issues-view-store";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { useClearFiltersOnWorkspaceChange } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { ListView } from "../issues/components/list-view";
import { BatchActionToolbar } from "../issues/components/batch-action-toolbar";
import { IssueDisplayControls } from "../issues/components/issues-header";
import { filterIssues } from "../issues/utils/filter";
import { matchesPinyin } from "../editor/extensions/pinyin-match";
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
  const setViewMode = useStore(actorIssuesViewStore, (s) => s.setViewMode);
  const statusFilters = useStore(actorIssuesViewStore, (s) => s.statusFilters);
  const priorityFilters = useStore(actorIssuesViewStore, (s) => s.priorityFilters);
  const assigneeFilters = useStore(actorIssuesViewStore, (s) => s.assigneeFilters);
  const includeNoAssignee = useStore(actorIssuesViewStore, (s) => s.includeNoAssignee);
  const creatorFilters = useStore(actorIssuesViewStore, (s) => s.creatorFilters);
  const projectFilters = useStore(actorIssuesViewStore, (s) => s.projectFilters);
  const includeNoProject = useStore(actorIssuesViewStore, (s) => s.includeNoProject);
  const labelFilters = useStore(actorIssuesViewStore, (s) => s.labelFilters);

  const [search, setSearch] = useState("");

  useClearFiltersOnWorkspaceChange(actorIssuesViewStore, wsId);

  // The actor tasks panel is list-only; clear any persisted "board" state
  // so list-only affordances (e.g. BatchActionToolbar) render correctly.
  useEffect(() => {
    setViewMode("list");
  }, [setViewMode]);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [scope, actorType, actorId]);

  const queryFilter: MyIssuesFilter = useMemo(
    () =>
      scope === "assigned"
        ? { assignee_id: actorId }
        : { creator_id: actorId },
    [scope, actorId],
  );
  const queryScope = `${actorType}:${actorId}:${scope}`;

  const rawIssuesQuery = useQuery(myIssueListOptions(wsId, queryScope, queryFilter));
  const rawIssues = useMemo(
    () => rawIssuesQuery.data ?? [],
    [rawIssuesQuery.data],
  );
  const isLoading = rawIssuesQuery.isLoading;

  const actorIssues = useMemo(
    () =>
      rawIssues.filter((issue) =>
        scope === "assigned"
          ? issue.assignee_type === actorType && issue.assignee_id === actorId
          : issue.creator_type === actorType && issue.creator_id === actorId,
      ),
    [actorId, actorType, rawIssues, scope],
  );

  const filteredIssues = useMemo(
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

  const issues = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return filteredIssues;
    return filteredIssues.filter((issue) => {
      const title = issue.title ?? "";
      return (
        title.toLowerCase().includes(query) ||
        issue.identifier.toLowerCase().includes(query) ||
        matchesPinyin(title, query)
      );
    });
  }, [filteredIssues, search]);

  const { data: childProgressMap = new Map() } = useQuery(
    childIssueProgressOptions(wsId),
  );

  const visibleStatuses = useMemo(() => {
    if (statusFilters.length > 0) {
      return BOARD_STATUSES.filter((s) => statusFilters.includes(s));
    }
    return BOARD_STATUSES;
  }, [statusFilters]);

  if (isLoading) {
    return <ActorIssuesSkeleton />;
  }

  return (
    <ViewStoreProvider store={actorIssuesViewStore}>
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center justify-between gap-3 border-b px-4">
          <div className="flex items-center gap-3">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={t(($) => $.actor_issues.search_placeholder)}
                className="h-8 w-64 pl-8 text-sm"
              />
            </div>
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
          </div>
          <IssueDisplayControls scopedIssues={actorIssues} hideViewToggle />
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
        ) : search.trim() !== "" && issues.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <Search className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.actor_issues.search_empty)}</p>
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 flex-col p-1">
            <ListView
              issues={issues}
              visibleStatuses={visibleStatuses}
              childProgressMap={childProgressMap}
              myIssuesScope={queryScope}
              myIssuesFilter={queryFilter}
            />
          </div>
        )}
        <BatchActionToolbar issues={issues} />
      </div>
    </ViewStoreProvider>
  );
}

function ActorIssuesSkeleton() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <div className="flex items-center gap-3">
          <Skeleton className="h-8 w-64 rounded-md" />
          <Skeleton className="h-8 w-20 rounded-md" />
          <Skeleton className="h-8 w-20 rounded-md" />
        </div>
        <div className="flex items-center gap-1">
          <Skeleton className="h-8 w-8 rounded-md" />
          <Skeleton className="h-8 w-8 rounded-md" />
        </div>
      </div>
      <div className="flex flex-1 min-h-0 flex-col gap-2 p-4">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full rounded-md" />
        ))}
      </div>
    </div>
  );
}
