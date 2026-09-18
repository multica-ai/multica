import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
import type { PRPolicy, PRPolicyUpdate } from "../api/pr-automation";

export const prAutomationKeys = {
  all: ["pr-automation"] as const,
  workspace: (wsId: string) => ["pr-automation", wsId] as const,
  issue: (wsId: string, issueId: string) =>
    ["pr-automation", wsId, "issue", issueId] as const,
};
export const prPolicyOptions = (wsId: string) =>
  queryOptions({
    queryKey: prAutomationKeys.workspace(wsId),
    queryFn: () => api.getPRPolicy(wsId),
    enabled: !!wsId,
  });
export const issuePRPolicyOptions = (wsId: string, issueId: string) =>
  queryOptions({
    queryKey: prAutomationKeys.issue(wsId, issueId),
    queryFn: () => api.getIssuePRPolicy(issueId),
    enabled: !!wsId && !!issueId,
  });
export function usePRPolicyMutations(wsId: string) {
  const qc = useQueryClient();
  return {
    sync: useMutation({
      mutationFn: () => api.syncPRPolicy(wsId),
      onSuccess: async () => {
        await Promise.all([
          qc.invalidateQueries({ queryKey: prAutomationKeys.workspace(wsId) }),
          qc.invalidateQueries({ queryKey: ["issues"] }),
          qc.invalidateQueries({ queryKey: ["github"] }),
        ]);
      },
    }),
    preview: useMutation({
      mutationFn: (policy: PRPolicy) => api.previewPRPolicy(wsId, policy),
    }),
    apply: useMutation({
      mutationFn: ({ policy, token }: { policy: PRPolicy; token: string }) =>
        api.updatePRPolicy(wsId, policy, token),
      onSuccess: async () => {
        await Promise.all([
          qc.invalidateQueries({ queryKey: prAutomationKeys.workspace(wsId) }),
          qc.invalidateQueries({ queryKey: ["issues"] }),
          qc.invalidateQueries({ queryKey: ["github"] }),
        ]);
      },
    }),
  };
}
export function useUpdateIssuePRPolicy(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: PRPolicyUpdate) =>
      api.updateIssuePRPolicy(issueId, body),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: prAutomationKeys.workspace(wsId) }),
        qc.invalidateQueries({
          queryKey: ["github", "pull-requests", issueId],
        }),
        qc.invalidateQueries({ queryKey: ["issues"] }),
      ]);
    },
  });
}
