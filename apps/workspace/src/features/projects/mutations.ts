import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import type { CreateProjectRequest, Project, UpdateProjectRequest } from "@/shared/types";
import { useWorkspaceStore } from "@/features/workspace";

export function useCreateProjectMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useMutation({
    mutationFn: (data: CreateProjectRequest) => api.createProject(data),
    onSuccess: (project) => {
      if (workspaceId) {
        queryClient.setQueryData(queryKeys.projects.detail(workspaceId, project.id), project);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects.all() });
    },
  });
}

export function useUpdateProjectMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateProjectRequest) => api.updateProject(id, data),
    onSuccess: (project) => {
      if (workspaceId) {
        queryClient.setQueryData<Project>(queryKeys.projects.detail(workspaceId, project.id), project);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects.all() });
    },
  });
}

export function useDeleteProjectMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useMutation({
    mutationFn: (id: string) => api.deleteProject(id),
    onSuccess: (_data, id) => {
      if (workspaceId) {
        queryClient.removeQueries({ queryKey: queryKeys.projects.detail(workspaceId, id) });
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects.all() });
    },
  });
}