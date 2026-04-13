import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateSandboxConfigRequest, UpdateSandboxConfigRequest } from "../types";
import { workspaceKeys } from "./queries";
import { runtimeKeys } from "../runtimes/queries";

export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { name: string; slug: string; description?: string }) =>
      api.createWorkspace(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useLeaveWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.leaveWorkspace(workspaceId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.deleteWorkspace(workspaceId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useCreateSandboxConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateSandboxConfigRequest) =>
      api.createSandboxConfig(wsId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfigs(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfig(wsId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    },
  });
}

export function useUpdateSandboxConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ configId, data }: { configId: string; data: UpdateSandboxConfigRequest }) =>
      api.updateSandboxConfig(wsId, configId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfigs(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfig(wsId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    },
  });
}

export function useDeleteSandboxConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (configId?: string) => api.deleteSandboxConfig(wsId, configId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfigs(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfig(wsId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    },
  });
}
