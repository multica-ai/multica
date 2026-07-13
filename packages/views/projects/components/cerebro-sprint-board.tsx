"use client";

// CEREBRO-PATCH(sprint-board-page): TECH-3684 dedicated sprint view.
// A sprint opens as its own board — the SAME project board machinery
// (inline create-in-sprint + drag-and-drop), pre-scoped and locked to one
// cerebro_sprint. Lives next to ProjectDetail so it can reuse the exported
// ProjectIssuesSurface without crossing the cerebro→views package boundary.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { CalendarRange } from "lucide-react";

import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectDetailOptions } from "@multica/core/projects/queries";
import type { MyIssuesFilter } from "@multica/core/issues/queries";
import { createIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { sprintOptions } from "@multica/cerebro-sprints/core/queries";
import type { SprintStatus } from "@multica/cerebro-sprints/core";
// CEREBRO-PATCH(sprint-header-date-range): FIR-2817 compact "DD-MM - DD-MM" date range instead of raw ISO strings.
import { formatSprintDateRange } from "@multica/cerebro-sprints/core";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";

import { ProjectIssuesSurface } from "./project-detail";
// CEREBRO-PATCH(sprint-sidebar): FIR-2828 wire in the sprint completion sidebar.
import { SprintSidebar } from "./cerebro-sprint-sidebar";
// CEREBRO-PATCH(sprint-header-breadcrumb): FIR-2817 reuse the same header chrome as the project page.
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";

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
      {/* CEREBRO-PATCH(sprint-header-breadcrumb): FIR-2817 same BreadcrumbHeader chrome as the project page (sticky bar, mobile sidebar trigger, chevron breadcrumb) instead of a hand-rolled header. */}
      <BreadcrumbHeader
        segments={[
          {
            href: wsPaths.projectDetail(projectId),
            label: projectQuery.data?.title ?? "Project",
          },
        ]}
        leaf={
          <div className="flex min-w-0 items-center gap-2">
            <CalendarRange className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate font-medium text-foreground">{sprint.name}</span>
            <Badge variant="secondary" className={cn("shrink-0", statusConfig.className)}>
              {statusConfig.label}
            </Badge>
            <span className="truncate text-xs text-muted-foreground">
              {/* CEREBRO-PATCH(sprint-header-date-range): FIR-2817 "DD-MM - DD-MM" instead of raw ISO dates. */}
              {formatSprintDateRange(sprint.start_date, sprint.end_date)}
              {sprint.goal ? ` · ${sprint.goal}` : ""}
            </span>
          </div>
        }
      />
      <div className="flex flex-1 overflow-hidden">
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
        {/* CEREBRO-PATCH(sprint-sidebar): FIR-2828 wire in the sprint completion sidebar. */}
        <SprintSidebar workspaceId={wsId} projectId={projectId} sprint={sprint} />
      </div>
    </div>
  );
}
