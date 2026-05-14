"use client";

import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { TasksFilters } from "./components/tasks-filters";
import { TasksTable } from "./components/tasks-table";
import { TasksPagination } from "./components/tasks-pagination";
import { useCerebroTasksStore } from "../core/store";
import { cerebroTasksListOptions } from "../core/queries";

// Cross-agent tasks page (JEH-900). Lists every agent task in the
// workspace with filters for agent, status, time range, and type. Backed
// by /api/cerebro/tasks. Hidden when the cerebro_tasks feature flag is
// off.
export function TasksPage() {
  const enabled = useFeatureFlag("cerebro_tasks");
  const workspace = useCurrentWorkspace();

  // Select primitives separately — returning a fresh object from the store
  // each render would loop. See packages/core/CLAUDE.md "Common Zustand
  // footguns".
  const agentId = useCerebroTasksStore((s) => s.agentId);
  const issueId = useCerebroTasksStore((s) => s.issueId);
  const projectId = useCerebroTasksStore((s) => s.projectId);
  const status = useCerebroTasksStore((s) => s.status);
  const type = useCerebroTasksStore((s) => s.type);
  const range = useCerebroTasksStore((s) => s.range);
  const customFrom = useCerebroTasksStore((s) => s.customFrom);
  const customTo = useCerebroTasksStore((s) => s.customTo);
  const limit = useCerebroTasksStore((s) => s.limit);
  const offset = useCerebroTasksStore((s) => s.offset);
  const search = useCerebroTasksStore((s) => s.search);
  const groupBy = useCerebroTasksStore((s) => s.groupBy);
  const visibleColumns = useCerebroTasksStore((s) => s.visibleColumns);

  const wsId = workspace?.id ?? "";
  const list = useQuery(
    cerebroTasksListOptions(wsId, {
      agentId,
      issueId,
      projectId,
      status,
      type,
      range,
      customFrom,
      customTo,
      search,
      limit,
      offset,
    }),
  );

  if (!enabled) return null;

  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  const tasks = list.data?.tasks ?? [];
  const total = list.data?.total ?? 0;

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">Tasks</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Alle agent-tasks på tværs af workspace
          </p>
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col gap-4 p-6">
          <TasksFilters wsId={wsId} />

          <TasksTable
            tasks={tasks}
            isLoading={list.isLoading || list.isFetching}
            isError={list.isError}
            errorMessage={list.error instanceof Error ? list.error.message : undefined}
            workspaceSlug={workspace.slug}
            visibleColumns={visibleColumns}
            groupBy={groupBy}
          />

          {!list.isError && (
            <TasksPagination total={total} loadedCount={tasks.length} />
          )}
        </div>
      </div>
    </div>
  );
}
