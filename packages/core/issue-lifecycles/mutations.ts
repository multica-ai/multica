import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { projectKeys } from "../projects/queries";
import type { IssueLifecycleResponse } from "../types";
import { issueLifecycleKeys } from "./queries";

export function useUpdateProjectIssueLifecycle() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      projectId,
      mode,
    }: {
      projectId: string;
      mode: "default" | "custom";
    }) => api.updateProjectIssueLifecycle(projectId, mode),
    onSuccess: (lifecycle, { projectId }) => {
      for (const [key] of qc.getQueriesData<IssueLifecycleResponse>({
        queryKey: issueLifecycleKeys.effective(wsId, projectId),
      })) {
        const options = key[key.length - 1] as { includeArchived?: boolean } | undefined;
        qc.setQueryData<IssueLifecycleResponse>(key, {
          ...lifecycle,
          statuses: options?.includeArchived
            ? lifecycle.statuses
            : lifecycle.statuses.filter((status) => !status.archived_at),
        });
      }
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
    onError: (_error, { projectId }) => {
      qc.invalidateQueries({
        queryKey: issueLifecycleKeys.effective(wsId, projectId),
      });
    },
  });
}
