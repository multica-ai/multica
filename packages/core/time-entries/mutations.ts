import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { timeEntryKeys } from "./queries";
import type { CreateTimeEntryRequest } from "../types";

export function useCreateTimeEntry() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      issueId,
      data,
    }: {
      issueId: string;
      data: CreateTimeEntryRequest;
    }) => api.createTimeEntry(issueId, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({
        queryKey: timeEntryKeys.issueEntries(wsId, vars.issueId),
      });
    },
  });
}

export function useDeleteTimeEntry() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { entryId: string; issueId: string }) =>
      api.deleteTimeEntry(vars.entryId),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({
        queryKey: timeEntryKeys.issueEntries(wsId, vars.issueId),
      });
    },
  });
}
