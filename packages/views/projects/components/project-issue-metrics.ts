import type { Issue } from "@multica/core/types";

export function getProjectIssueMetrics(projectIssues: Issue[]) {
  const totalCount = projectIssues.length;
  const completedCount = projectIssues.filter((issue) => issue.status === "done").length;
  return {
    totalCount,
    completedCount,
    doneColumnCount: completedCount,
  };
}
