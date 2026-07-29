"use client";

// CEREBRO-PATCH(issue-table-surface): FIR-3447 mount upstream's configurable
// Table on the fork's existing Issues/My Issues/Project query and filter seams.

import { useCallback, useDeferredValue, useMemo, useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  childIssueProgressOptions,
  issueFlatExportOptions,
  issueFlatListOptions,
  type IssueFlatFilter,
  type IssueSortParam,
} from "@multica/core/issues/queries";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { projectListOptions } from "@multica/core/projects";
import type { Project } from "@multica/core/types";
import type { ChildProgress } from "./list-row";
import { BatchActionToolbar } from "./batch-action-toolbar";
import { TableView } from "./table-view";

export function IssueTableSurface({
  scope,
  baseFilter,
  userId,
  sort,
  runningIssueIds,
  childProgressMap,
}: {
  scope: string;
  baseFilter: IssueFlatFilter;
  userId?: string;
  sort?: IssueSortParam;
  runningIssueIds: ReadonlySet<string>;
  childProgressMap: Map<string, ChildProgress>;
}) {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search.trim());
  const statusFilters = useViewStore((state) => state.statusFilters ?? []);
  const priorityFilters = useViewStore((state) => state.priorityFilters ?? []);
  const assigneeFilters = useViewStore((state) => state.assigneeFilters ?? []);
  const includeNoAssignee = useViewStore((state) => state.includeNoAssignee);
  const creatorFilters = useViewStore((state) => state.creatorFilters ?? []);
  const projectFilters = useViewStore((state) => state.projectFilters ?? []);
  const includeNoProject = useViewStore((state) => state.includeNoProject);
  const labelFilters = useViewStore((state) => state.labelFilters ?? []);
  const propertyFilters = useViewStore((state) => state.propertyFilters ?? {});
  const agentRunningFilter = useViewStore((state) => state.agentRunningFilter);
  const subIssueDisplay = useViewStore(
    (state) => state.subIssueDisplay ?? "on-parent",
  );

  const flatFilter = useMemo<IssueFlatFilter>(
    () => ({
      ...baseFilter,
      q: deferredSearch || undefined,
      statuses: statusFilters.length > 0 ? statusFilters : undefined,
      priorities: priorityFilters.length > 0 ? priorityFilters : undefined,
      assignee_filters: assigneeFilters.length > 0 ? assigneeFilters : undefined,
      include_no_assignee: includeNoAssignee || undefined,
      creator_filters: creatorFilters.length > 0 ? creatorFilters : undefined,
      project_ids: projectFilters.length > 0 ? projectFilters : undefined,
      include_no_project: includeNoProject || undefined,
      label_ids: labelFilters.length > 0 ? labelFilters : undefined,
      properties: Object.keys(propertyFilters).length > 0 ? propertyFilters : undefined,
      top_level_only: subIssueDisplay === "hidden" || undefined,
      ids: agentRunningFilter ? [...runningIssueIds] : undefined,
    }),
    [
      agentRunningFilter,
      assigneeFilters,
      baseFilter,
      creatorFilters,
      deferredSearch,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      priorityFilters,
      projectFilters,
      propertyFilters,
      runningIssueIds,
      statusFilters,
      subIssueDisplay,
    ],
  );

  const tableQuery = useInfiniteQuery(
    issueFlatListOptions(wsId, scope, flatFilter, userId, sort),
  );
  const issues = useMemo(
    () => tableQuery.data?.pages.flatMap((page) => page.issues) ?? [],
    [tableQuery.data],
  );
  const total = tableQuery.data?.pages.at(-1)?.total ?? 0;
  const exportIssues = useCallback(
    () =>
      queryClient.fetchQuery(
        issueFlatExportOptions(wsId, scope, flatFilter, userId, sort),
      ),
    [flatFilter, queryClient, scope, sort, userId, wsId],
  );
  const resolveExportLookups = useCallback(
    async ({ projects, childProgress }: { projects: boolean; childProgress: boolean }) => {
      const [projectResponse, progressResponse] = await Promise.all([
        projects
          ? queryClient.fetchQuery(projectListOptions(wsId))
          : Promise.resolve(null),
        childProgress
          ? queryClient.fetchQuery(childIssueProgressOptions(wsId))
          : Promise.resolve(null),
      ]);
      const projectMap = new Map<string, Project>();
      for (const project of projectResponse?.projects ?? []) {
        projectMap.set(project.id, project);
      }
      const childProgressMap = new Map<string, ChildProgress>();
      for (const progress of progressResponse?.progress ?? []) {
        childProgressMap.set(progress.parent_issue_id, {
          done: progress.done,
          total: progress.total,
        });
      }
      return {
        projectMap,
        childProgressMap,
      };
    },
    [queryClient, wsId],
  );

  return (
    <>
      <TableView
        issues={issues}
        childProgressMap={childProgressMap}
        fetchNextPage={tableQuery.fetchNextPage}
        hasNextPage={tableQuery.hasNextPage}
        isFetchingNextPage={tableQuery.isFetchingNextPage}
        windowError={tableQuery.isError}
        total={total}
        search={search}
        onSearchChange={setSearch}
        exportIssues={exportIssues}
        resolveExportLookups={resolveExportLookups}
      />
      <BatchActionToolbar issues={issues} />
    </>
  );
}
