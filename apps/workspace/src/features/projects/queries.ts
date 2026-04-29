import { useQuery } from "@tanstack/react-query";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import type { ListProjectsResponse, Project, ProjectStatus } from "@/shared/types";
import { useWorkspaceStore } from "@/features/workspace";

export function useProjectsQuery(status?: ProjectStatus) {
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useQuery<ListProjectsResponse, Error, Project[]>({
    queryKey: queryKeys.projects.list(workspaceId ?? "", status ? { status } : undefined),
    queryFn: () => api.listProjects(status ? { status } : undefined),
    enabled: Boolean(workspaceId),
    select: (data) => data.projects,
  });
}

export function useProjectQuery(projectId?: string) {
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useQuery<Project>({
    queryKey: queryKeys.projects.detail(workspaceId ?? "", projectId ?? ""),
    queryFn: () => api.getProject(projectId!),
    enabled: Boolean(workspaceId && projectId),
  });
}