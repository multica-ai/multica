import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { CreateSkillRequest, Skill, UpdateSkillRequest } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";

function upsertSkillInWorkspaceList(skills: Skill[], nextSkill: Skill): Skill[] {
  const index = skills.findIndex((skill) => skill.id === nextSkill.id);
  if (index === -1) {
    return [...skills, nextSkill];
  }

  const next = [...skills];
  next[index] = nextSkill;
  return next;
}

export function useSkillMutations() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id ?? null);

  const createSkillMutation = useMutation({
    mutationFn: (data: CreateSkillRequest) => api.createSkill(data),
    onSuccess: (skill) => {
      if (!workspaceId) return;
      queryClient.setQueryData<Skill[]>(queryKeys.workspace.skills(workspaceId), (existing = []) =>
        upsertSkillInWorkspaceList(existing, skill),
      );
      queryClient.setQueryData(queryKeys.workspace.skillDetail(workspaceId, skill.id), skill);
    },
  });

  const importSkillMutation = useMutation({
    mutationFn: (data: { url: string }) => api.importSkill(data),
    onSuccess: (skill) => {
      if (!workspaceId) return;
      queryClient.setQueryData<Skill[]>(queryKeys.workspace.skills(workspaceId), (existing = []) =>
        upsertSkillInWorkspaceList(existing, skill),
      );
      queryClient.setQueryData(queryKeys.workspace.skillDetail(workspaceId, skill.id), skill);
    },
  });

  const updateSkillMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateSkillRequest }) => api.updateSkill(id, data),
    onSuccess: (skill) => {
      if (!workspaceId) return;
      queryClient.setQueryData<Skill[]>(queryKeys.workspace.skills(workspaceId), (existing = []) =>
        upsertSkillInWorkspaceList(existing, skill),
      );
      queryClient.setQueryData(queryKeys.workspace.skillDetail(workspaceId, skill.id), skill);
    },
  });

  const deleteSkillMutation = useMutation({
    mutationFn: (id: string) => api.deleteSkill(id),
    onSuccess: (_result, id) => {
      if (!workspaceId) return;
      queryClient.setQueryData<Skill[]>(queryKeys.workspace.skills(workspaceId), (existing = []) =>
        existing.filter((skill) => skill.id !== id),
      );
      queryClient.removeQueries({ queryKey: queryKeys.workspace.skillDetail(workspaceId, id) });
    },
  });

  return {
    createSkill: (data: CreateSkillRequest) => createSkillMutation.mutateAsync(data),
    importSkill: (data: { url: string }) => importSkillMutation.mutateAsync(data),
    updateSkill: (id: string, data: UpdateSkillRequest) => updateSkillMutation.mutateAsync({ id, data }),
    deleteSkill: (id: string) => deleteSkillMutation.mutateAsync(id),
  };
}
