import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { RuntimePing, RuntimeUpdate } from "../types";
import { runtimeKeys } from "./queries";

export function useDeleteRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runtimeId: string) => api.deleteRuntime(runtimeId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

export function usePingRuntime(runtimeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<RuntimePing> => {
      const ping = await api.pingRuntime(runtimeId);

      return new Promise((resolve) => {
        const poll = setInterval(async () => {
          try {
            const result = await api.getPingResult(runtimeId, ping.id);
            if (
              result.status === "completed" ||
              result.status === "failed" ||
              result.status === "timeout"
            ) {
              clearInterval(poll);
              resolve(result);
            }
          } catch {
            // ignore poll errors
          }
        }, 2000);
      });
    },
    onMutate: () => {
      // Set a pending state in the cache so the UI shows progress immediately
      qc.setQueryData<RuntimePing>(runtimeKeys.pingResult(runtimeId), (old) => ({
        id: "",
        runtime_id: runtimeId,
        status: "pending",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        ...old,
        // Force status to pending on new mutation
      } as RuntimePing));
    },
    onSuccess: (result) => {
      qc.setQueryData<RuntimePing>(runtimeKeys.pingResult(runtimeId), result);
    },
    onError: () => {
      qc.setQueryData<RuntimePing>(runtimeKeys.pingResult(runtimeId), {
        id: "",
        runtime_id: runtimeId,
        status: "failed",
        error: "Failed to initiate test",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      });
    },
  });
}

export function useUpdateRuntime(runtimeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (targetVersion: string): Promise<RuntimeUpdate> => {
      const update = await api.initiateUpdate(runtimeId, targetVersion);

      return new Promise((resolve) => {
        const poll = setInterval(async () => {
          try {
            const result = await api.getUpdateResult(runtimeId, update.id);
            if (
              result.status === "completed" ||
              result.status === "failed" ||
              result.status === "timeout"
            ) {
              clearInterval(poll);
              resolve(result);
            }
          } catch {
            // ignore poll errors
          }
        }, 2000);
      });
    },
    onMutate: () => {
      qc.setQueryData<RuntimeUpdate>(runtimeKeys.updateResult(runtimeId), {
        id: "",
        runtime_id: runtimeId,
        status: "pending",
        target_version: "",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      });
    },
    onSuccess: (result) => {
      qc.setQueryData<RuntimeUpdate>(runtimeKeys.updateResult(runtimeId), result);
    },
    onError: () => {
      qc.setQueryData<RuntimeUpdate>(runtimeKeys.updateResult(runtimeId), {
        id: "",
        runtime_id: runtimeId,
        status: "failed",
        target_version: "",
        error: "Failed to initiate update",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      });
    },
  });
}
