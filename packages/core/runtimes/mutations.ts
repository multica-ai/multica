import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { RuntimeVisibility } from "../types";
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

export function useUpdateRuntimeVisibility(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      runtimeId,
      visibility,
    }: {
      runtimeId: string;
      visibility: RuntimeVisibility;
    }) => api.updateRuntime(runtimeId, { visibility }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}
