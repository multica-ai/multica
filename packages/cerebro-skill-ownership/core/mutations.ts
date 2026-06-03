import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { skillOwnershipKeys } from "./queries";
import type {
  CreateSkillChangeRequestRequest,
  ForkSkillRequest,
  ReviewSkillChangeRequestRequest,
  UpdateSkillOwnershipRequest,
} from "@multica/core/types";

export function useUpdateSkillOwnership(skillId: string, wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: UpdateSkillOwnershipRequest) =>
      api.updateSkillOwnership(skillId, data),
    onSuccess: (updated) => {
      qc.setQueryData(
        [...workspaceKeys.skills(wsId), skillId],
        updated,
      );
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
    },
  });
}

export function useCreateSkillChangeRequest(skillId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateSkillChangeRequestRequest) =>
      api.createSkillChangeRequest(skillId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: skillOwnershipKeys.changeRequests(skillId) });
      qc.invalidateQueries({ queryKey: skillOwnershipKeys.pendingAll() });
    },
  });
}

export function useReviewSkillChangeRequest(skillId: string, wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      crId,
      data,
    }: {
      crId: string;
      data: ReviewSkillChangeRequestRequest;
    }) => api.reviewSkillChangeRequest(crId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: skillOwnershipKeys.changeRequests(skillId) });
      qc.invalidateQueries({ queryKey: skillOwnershipKeys.pendingAll() });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
    },
  });
}

export function useCreateSkillFork(skillId: string, wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: ForkSkillRequest) => api.createSkillFork(skillId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: skillOwnershipKeys.forks(skillId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
    },
  });
}
