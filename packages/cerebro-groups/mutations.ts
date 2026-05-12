import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { groupKeys } from "./queries";
import type {
  CerebroGroup,
  CreateCerebroGroupRequest,
  UpdateCerebroGroupRequest,
} from "./types";

export function useCreateCerebroGroup() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: CreateCerebroGroupRequest) =>
      api.createCerebroGroup<CerebroGroup>(wsId, body),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.list(wsId) });
    },
  });
}

export function useUpdateCerebroGroup() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { id: string; body: UpdateCerebroGroupRequest }) =>
      api.updateCerebroGroup<CerebroGroup>(vars.id, vars.body),
    onSuccess: (updated) => {
      // Optimistic-friendly merge: rewrite the row in place so a refetch isn't
      // strictly required for the detail pane to show the new name/description.
      qc.setQueryData<CerebroGroup[]>(groupKeys.list(wsId), (old) =>
        old?.map((g) => (g.id === updated.id ? updated : g)),
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.list(wsId) });
    },
  });
}

export function useDeleteCerebroGroup() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteCerebroGroup(id),
    onSuccess: (_void, id) => {
      qc.setQueryData<CerebroGroup[]>(groupKeys.list(wsId), (old) =>
        old?.filter((g) => g.id !== id),
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.list(wsId) });
    },
  });
}

export function useAddCerebroGroupMember(groupId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (userId: string) =>
      api.addCerebroGroupMember(groupId, userId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.members(wsId, groupId) });
    },
  });
}

export function useRemoveCerebroGroupMember(groupId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (userId: string) =>
      api.removeCerebroGroupMember(groupId, userId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.members(wsId, groupId) });
    },
  });
}

// JEH-1009 — permission-row mutations. The server publishes
// group:capability_changed / group:runtime_changed / group:agent_changed
// after each mutation; the cerebro-realtime registration in
// `realtime.ts` will route those to query invalidations.

export function useSetCerebroGroupCapability(groupId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (capability: string) =>
      api.setCerebroGroupCapability(groupId, capability),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.capabilities(wsId, groupId) });
    },
  });
}

export function useRemoveCerebroGroupCapability(groupId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (capability: string) =>
      api.removeCerebroGroupCapability(groupId, capability),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.capabilities(wsId, groupId) });
    },
  });
}

export function useAddCerebroGroupRuntime(groupId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (runtimeId: string) =>
      api.addCerebroGroupRuntime(groupId, runtimeId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.runtimes(wsId, groupId) });
    },
  });
}

export function useRemoveCerebroGroupRuntime(groupId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (runtimeId: string) =>
      api.removeCerebroGroupRuntime(groupId, runtimeId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.runtimes(wsId, groupId) });
    },
  });
}

export function useAddCerebroGroupAgent(groupId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (agentId: string) =>
      api.addCerebroGroupAgent(groupId, agentId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.agents(wsId, groupId) });
    },
  });
}

export function useRemoveCerebroGroupAgent(groupId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (agentId: string) =>
      api.removeCerebroGroupAgent(groupId, agentId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: groupKeys.agents(wsId, groupId) });
    },
  });
}

export function useAddCerebroProjectGroup(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (groupId: string) =>
      api.addCerebroProjectGroup(projectId, groupId),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: groupKeys.projectGroups(wsId, projectId),
      });
    },
  });
}

export function useRemoveCerebroProjectGroup(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (groupId: string) =>
      api.removeCerebroProjectGroup(projectId, groupId),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: groupKeys.projectGroups(wsId, projectId),
      });
    },
  });
}
