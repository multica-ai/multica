import { useMutation } from "@tanstack/react-query";
import { api } from "@/shared/api";

export function useRuntimeMutations() {
  const pingRuntimeMutation = useMutation({
    mutationFn: (runtimeId: string) => api.pingRuntime(runtimeId),
  });

  const getPingResultMutation = useMutation({
    mutationFn: ({ runtimeId, pingId }: { runtimeId: string; pingId: string }) =>
      api.getPingResult(runtimeId, pingId),
  });

  const initiateUpdateMutation = useMutation({
    mutationFn: ({ runtimeId, targetVersion }: { runtimeId: string; targetVersion: string }) =>
      api.initiateUpdate(runtimeId, targetVersion),
  });

  const getUpdateResultMutation = useMutation({
    mutationFn: ({ runtimeId, updateId }: { runtimeId: string; updateId: string }) =>
      api.getUpdateResult(runtimeId, updateId),
  });

  return {
    pingRuntime: (runtimeId: string) => pingRuntimeMutation.mutateAsync(runtimeId),
    getPingResult: (runtimeId: string, pingId: string) =>
      getPingResultMutation.mutateAsync({ runtimeId, pingId }),
    initiateUpdate: (runtimeId: string, targetVersion: string) =>
      initiateUpdateMutation.mutateAsync({ runtimeId, targetVersion }),
    getUpdateResult: (runtimeId: string, updateId: string) =>
      getUpdateResultMutation.mutateAsync({ runtimeId, updateId }),
  };
}
