import type { Project, ProjectProgress } from "@multica/core/types";

/**
 * The project's progress summary. Servers that predate `progress` only send
 * issue_count / done_count, where done_count also counts cancelled issues;
 * that older ratio is the best available there.
 */
export function getProjectProgress(
  project: Pick<Project, "issue_count" | "done_count" | "progress">,
): ProjectProgress {
  if (project.progress) return project.progress;
  const scope = project.issue_count;
  const completed = Math.min(project.done_count, scope);
  return {
    unit: "issues",
    scope,
    completed,
    started: 0,
    unstarted: scope - completed,
    cancelled: 0,
    percent: scope > 0 ? Math.floor((completed * 100) / scope) : 0,
  };
}
