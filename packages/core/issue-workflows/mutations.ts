import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { projectKeys } from "../projects/queries";
import { issueKeys } from "../issues/queries";
import type { IssueWorkflowResponse } from "../types";
import { effectiveIssueWorkflowOptions, issueWorkflowKeys } from "./queries";

export function useApplyProjectWorkflow() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ projectId, data }: { projectId: string; data: import("../types").ApplyProjectWorkflowRequest }) => api.applyProjectWorkflow(projectId, data),
    onSuccess: (workflow, { projectId }) => {
      for (const includeArchived of [false, true]) {
        qc.setQueriesData<IssueWorkflowResponse>({
          queryKey: effectiveIssueWorkflowOptions(wsId, projectId, includeArchived).queryKey,
          exact: true,
        }, {
          ...workflow,
          statuses: includeArchived
            ? workflow.statuses
            : workflow.statuses.filter((status) => !status.archived_at),
        });
      }
      qc.invalidateQueries({ queryKey: issueWorkflowKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}
