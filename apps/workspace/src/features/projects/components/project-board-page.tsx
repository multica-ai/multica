"use client";

import { Navigate } from "@tanstack/react-router";
import { WorkbenchIssuesPage } from "@/features/issues/components/workbench-issues-page";
import { getProjectBoardViewStore } from "@/features/issues/stores/workbench-view-stores";
import { useProjectQuery } from "../queries";

export function ProjectBoardPage({ projectId }: { projectId: string }) {
  const { data: project, isLoading } = useProjectQuery(projectId);

  if (isLoading) {
    return <div className="flex flex-1 min-h-0 items-center justify-center text-sm text-muted-foreground">Loading project board...</div>;
  }

  if (!project) {
    return <Navigate to="/projects" replace />;
  }

  return (
    <WorkbenchIssuesPage
      breadcrumbLabel={`${project.title} Board`}
      emptyTitle="No work in this project"
      emptyDescription="Create an issue from this board or link existing work to the project."
      store={getProjectBoardViewStore(projectId)}
      deriveIssues={(issues) => issues.filter((issue) => issue.project_id === projectId)}
      forcedViewMode="board"
      hideViewToggle
      createIssueData={{ project_id: projectId }}
    />
  );
}