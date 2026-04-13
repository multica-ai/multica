import type { Issue, Project } from "@multica/core/types";

export function getProjectIssueMetrics(
  project: Pick<Project, "issue_count" | "done_count">,
  projectIssues: Issue[],
) {
  return {
    totalCount: project.issue_count,
    completedCount: project.done_count,
    doneColumnCount: projectIssues.filter((issue) => issue.status === "done").length,
  };
}
