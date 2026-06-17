"use client";

// CEREBRO-PATCH(sprint-board-page): TECH-3684 dedicated sprint view.
// A sprint opens as its own board — the SAME project board machinery
// (inline create-in-sprint + drag-and-drop), pre-scoped and locked to one
// cerebro_sprint. Lives next to ProjectDetail so it can reuse the exported
// ProjectIssuesSurface without crossing the cerebro→views package boundary.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { CalendarRange, ArrowLeft } from "lucide-react";

import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectDetailOptions } from "@multica/core/projects/queries";
import type { MyIssuesFilter } from "@multica/core/issues/queries";
import { createIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { sprintOptions } from "@multica/cerebro-sprints/core/queries";
import type { SprintStatus } from "@multica/cerebro-sprints/core";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";

import { ProjectIssuesSurface } from "./project-detail";
import { AppLink } from "../../navigation";

// The sprint board keeps its own view state (filters, board/list mode) so it
// never bleeds into the project page's saved view.
const sprintViewStore = createIssueViewStore("sprint_board_view");

const SPRINT_STATUS_CONFIG: Record<SprintStatus, { label: string; className: string }> = {
  planned: { label: "Planned", className: "bg-muted text-muted-foreground" },
  active: { label: "Active", className: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  done: { label: "Done", className: "bg-blue-500/15 text-blue-600 dark:text-blue-400" },
  cancelled: { label: "Cancelled", className: "bg-destructive/15 text-destructive" },
};

export function SprintBoard({ sprintId }: { sprintId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const sprintQuery = useQuery(sprintOptions(wsId, sprintId));
  const sprint = sprintQuery.data;
  const projectId = sprint?.project_id ?? "";

  const projectQuery = useQuery({
    ...projectDetailOptions(wsId, projectId),
    enabled: !!projectId,
  });

  // Sprint membership lives in cerebro_sprint_issue, never on issue.project_id —
  // so the board stays scoped to the home project and the server narrows to the
  // sprint's members via sprint_id (seeded into ProjectIssuesSurface below).
  const projectFilter = useMemo<MyIssuesFilter>(
    () => ({ project_id: projectId }),
    [projectId],
  );

  if (sprintQuery.isLoading) {
    return <div className="p-6 text-sm text-muted-foreground">Loading sprint…</div>;
  }
  if (!sprint) {
    return (
      <div className="p-6 text-sm text-muted-foreground">
        This sprint no longer exists.
      </div>
    );
  }

  const statusConfig = SPRINT_STATUS_CONFIG[sprint.status] ?? SPRINT_STATUS_CONFIG.planned;

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-col gap-1 border-b px-4 py-3">
        <AppLink
          href={wsPaths.projectDetail(projectId)}
          className="flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-3" />
          {projectQuery.data?.title ?? "Project"}
        </AppLink>
        <div className="flex min-w-0 items-center gap-2">
          <CalendarRange className="size-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-lg font-medium">{sprint.name}</h1>
          <Badge variant="secondary" className={cn(statusConfig.className)}>
            {statusConfig.label}
          </Badge>
          <span className="truncate text-xs text-muted-foreground">
            {sprint.start_date} → {sprint.end_date}
            {sprint.goal ? ` · ${sprint.goal}` : ""}
          </span>
        </div>
      </div>
      <div className="flex flex-1 flex-col overflow-hidden">
        <ViewStoreProvider store={sprintViewStore}>
          {/* key forces a fresh seed when navigating between sprints. */}
          <ProjectIssuesSurface
            key={sprintId}
            projectId={projectId}
            scope={`project:${projectId}`}
            filter={projectFilter}
            initialSprintId={sprintId}
            lockSprint
          />
        </ViewStoreProvider>
      </div>
    </div>
  );
}
