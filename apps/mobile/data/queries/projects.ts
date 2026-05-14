/**
 * Workspace project list. Consumed by the read-only project chip on issue
 * detail and by `ProjectPickerSheet` in the new-issue / issue-detail flows.
 */
import { queryOptions } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";
import { api } from "@/data/api";

export const projectKeys = {
  all: (wsId: string | null) => ["projects", wsId] as const,
};

export const projectListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: projectKeys.all(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listProjects({ signal });
      return res.projects;
    },
    enabled: !!wsId,
  });

/**
 * Helper for the read-only project chip — returns the project matching id,
 * or undefined. Caller selects from the list query and looks up by id.
 */
export function findProject(
  projects: Project[],
  id: string | null,
): Project | undefined {
  if (!id) return undefined;
  return projects.find((p) => p.id === id);
}
