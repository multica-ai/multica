import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
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

// useUpdateRuntime patches editable fields on a runtime (currently the
// reporting timezone). Invalidates the runtime list AND any keys downstream
// of the updated runtime — usage queries are bucketed by tz on the server,
// so a tz change must blow away cached usage rows or the chart would lie
// for one polling cycle.
export function useUpdateRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      runtimeId,
      patch,
    }: {
      runtimeId: string;
      patch: { timezone?: string };
    }) => api.updateRuntime(runtimeId, patch),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      if (vars) {
        // Usage query keys are not workspace-scoped; invalidate only this
        // runtime's daily/by-agent/by-hour usage rows under the new tz buckets.
        qc.invalidateQueries({
          queryKey: ["runtimes", "usage", vars.runtimeId],
        });
        qc.invalidateQueries({
          queryKey: ["runtimes", "usage", "by-agent", vars.runtimeId],
        });
        qc.invalidateQueries({
          queryKey: ["runtimes", "usage", "by-hour", vars.runtimeId],
        });
      }
    },
  });
}
