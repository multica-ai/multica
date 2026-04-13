import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Workspace } from "../types";
import { api } from "../api";
import { workspaceKeys, workspaceListOptions } from "./queries";
import { useWorkspaceStore } from "./index";

export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { name: string; slug: string; description?: string }) =>
      api.createWorkspace(data),
    onSuccess: (newWs) => {
      // Add to cache before switching so sidebar list is consistent on first render
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] = []) => [...old, newWs]);
      useWorkspaceStore.getState().switchWorkspace(newWs);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useLeaveWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.leaveWorkspace(workspaceId),
    onSuccess: async (_, workspaceId) => {
      const currentWsId = useWorkspaceStore.getState().workspace?.id;
      if (currentWsId === workspaceId) {
        // staleTime: 0 forces a real network fetch — cache still has the left workspace
        const wsList = await qc.fetchQuery({ ...workspaceListOptions(), staleTime: 0 });
        useWorkspaceStore.getState().hydrateWorkspace(wsList);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.deleteWorkspace(workspaceId),
    onSuccess: async (_, workspaceId) => {
      const currentWsId = useWorkspaceStore.getState().workspace?.id;
      if (currentWsId === workspaceId) {
        // staleTime: 0 forces a real network fetch — cache still has the deleted workspace
        const wsList = await qc.fetchQuery({ ...workspaceListOptions(), staleTime: 0 });
        useWorkspaceStore.getState().hydrateWorkspace(wsList);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}
