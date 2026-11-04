import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { governanceKeys } from "./queries";
import type { ApprovalPolicy, CreateApprovalPolicyRequest, PolicyAutoApprovalHistory } from "./approval-policy";

export const approvalPoliciesKey = (wsId: string) => [...governanceKeys.all(wsId), "approval-policies"] as const;

/** The Policies list. */
export function approvalPoliciesOptions(wsId: string) {
  return queryOptions({
    queryKey: approvalPoliciesKey(wsId),
    queryFn: () => api.listApprovalPolicies(),
    enabled: !!wsId,
  });
}

/** A policy's auto-approval history, read when its row is opened. */
export function policyAutoApprovalsOptions(wsId: string, policyId: string) {
  return queryOptions({
    queryKey: [...approvalPoliciesKey(wsId), policyId, "auto-approvals"] as const,
    queryFn: (): Promise<PolicyAutoApprovalHistory> => api.listPolicyAutoApprovals(policyId),
    enabled: !!wsId && !!policyId,
  });
}

/** Creates a standing approval. The list refetches when the write settles. */
export function useCreateApprovalPolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation<ApprovalPolicy, Error, CreateApprovalPolicyRequest>({
    mutationFn: (data) => api.createApprovalPolicy(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: governanceKeys.all(wsId) });
    },
  });
}

/** Revokes a standing approval in one step. */
export function useRevokeApprovalPolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation<ApprovalPolicy, Error, string>({
    mutationFn: (policyId) => api.revokeApprovalPolicy(policyId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: governanceKeys.all(wsId) });
    },
  });
}
