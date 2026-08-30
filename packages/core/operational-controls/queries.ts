import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type {
  AgentToolActionListParams,
  AgentToolApprovalListParams,
  OperationalSummaryParams,
} from "./schemas";

export const operationalControlKeys = {
  all: (workspaceId: string) =>
    ["workspaces", workspaceId, "operational-controls"] as const,
  policies: (workspaceId: string) =>
    [...operationalControlKeys.all(workspaceId), "agents"] as const,
  policy: (workspaceId: string, agentId: string) =>
    [
      ...operationalControlKeys.policies(workspaceId),
      agentId,
      "policy",
    ] as const,
  actions: (
    workspaceId: string,
    agentId: string,
    params: AgentToolActionListParams = {},
  ) =>
    [
      ...operationalControlKeys.policies(workspaceId),
      agentId,
      "actions",
      params,
    ] as const,
  approvals: (
    workspaceId: string,
    params: AgentToolApprovalListParams = {},
  ) =>
    [...operationalControlKeys.all(workspaceId), "approvals", params] as const,
  capabilities: (workspaceId: string) =>
    [...operationalControlKeys.all(workspaceId), "capabilities"] as const,
  summary: (workspaceId: string, params: OperationalSummaryParams) =>
    [
      ...operationalControlKeys.all(workspaceId),
      "summary",
      params,
    ] as const,
};

export function agentToolPolicyOptions(
  workspaceId: string,
  agentId: string,
) {
  return queryOptions({
    queryKey: operationalControlKeys.policy(workspaceId, agentId),
    queryFn: () => api.getAgentToolPolicy(agentId),
    enabled: workspaceId.length > 0 && agentId.length > 0,
    staleTime: 0,
    retry: false,
  });
}

export function agentToolActionListOptions(
  workspaceId: string,
  agentId: string,
  params: AgentToolActionListParams = {},
) {
  return queryOptions({
    queryKey: operationalControlKeys.actions(workspaceId, agentId, params),
    queryFn: () => api.listAgentToolActions(agentId, params),
    enabled: workspaceId.length > 0 && agentId.length > 0,
    staleTime: 0,
    retry: false,
  });
}

export function agentToolApprovalListOptions(
  workspaceId: string,
  params: AgentToolApprovalListParams = {},
) {
  return queryOptions({
    queryKey: operationalControlKeys.approvals(workspaceId, params),
    queryFn: () => api.listAgentToolApprovals(params),
    enabled: workspaceId.length > 0,
    staleTime: 0,
    retry: false,
  });
}

export function operationalCapabilityListOptions(workspaceId: string) {
  return queryOptions({
    queryKey: operationalControlKeys.capabilities(workspaceId),
    queryFn: () => api.listOperationalCapabilities(),
    enabled: workspaceId.length > 0,
    staleTime: 0,
    retry: false,
  });
}

export function operationalSummaryOptions(
  workspaceId: string,
  params: OperationalSummaryParams,
) {
  return queryOptions({
    queryKey: operationalControlKeys.summary(workspaceId, params),
    queryFn: () => api.getOperationalSummary(params),
    enabled: workspaceId.length > 0,
    staleTime: 0,
    retry: false,
  });
}
