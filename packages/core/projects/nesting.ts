// CEREBRO-PATCH(nested-projects): fork-specific nested projects hooks.
// Kept in a dedicated file so upstream-merges don't conflict on project queries.

import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { ListProjectTreeResponse, ProjectTreeItem } from "../types";

const projectAllKey = (wsId: string) => ["projects", wsId] as const;

export const projectNestingKeys = {
  tree: (wsId: string) => [...projectAllKey(wsId), "tree"] as const,
  rollupStats: (wsId: string, projectId: string) =>
    [...projectAllKey(wsId), "rollup-stats", projectId] as const,
};

export function buildProjectTree(projects: ProjectTreeItem[]): ProjectTreeItem[] {
  const byId = new Map<string, ProjectTreeItem>();
  for (const project of projects) {
    byId.set(project.id, { ...project, children: [] });
  }

  const roots: ProjectTreeItem[] = [];
  for (const project of byId.values()) {
    if (project.parent_project_id && byId.has(project.parent_project_id)) {
      byId.get(project.parent_project_id)!.children!.push(project);
    } else {
      roots.push(project);
    }
  }

  const sortProjects = (items: ProjectTreeItem[]) => {
    items.sort((a, b) => a.title.localeCompare(b.title));
    for (const item of items) sortProjects(item.children ?? []);
  };
  sortProjects(roots);

  return roots;
}

export function projectTreeOptions(wsId: string) {
  return queryOptions({
    queryKey: projectNestingKeys.tree(wsId),
    queryFn: () => api.getProjectTree(),
    select: (data: ListProjectTreeResponse) => buildProjectTree(data.projects),
  });
}

export function projectRollupStatsOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectNestingKeys.rollupStats(wsId, projectId),
    queryFn: () => api.getProjectRollupStats(projectId),
  });
}

export function useSetProjectParent(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, parentProjectId }: { id: string; parentProjectId: string | null }) =>
      api.setProjectParent(id, parentProjectId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: projectAllKey(wsId) });
    },
  });
}

export function useSetProjectShowDescendants(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, showDescendants }: { id: string; showDescendants: boolean }) =>
      api.setProjectShowDescendants(id, showDescendants),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: projectAllKey(wsId) });
    },
  });
}
