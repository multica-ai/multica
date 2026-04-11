import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { UpsertSandboxConfigRequest } from "../types";
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

export function useUpsertSandboxConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: UpsertSandboxConfigRequest) =>
      api.upsertSandboxConfig(wsId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfig(wsId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    },
  });
}

export function useDeleteSandboxConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteSandboxConfig(wsId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfig(wsId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    },
  });
}
