import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { ReplaceFeishuProjectRoutesRequest } from "../types";

export const feishuProjectKeys = {
  all: (wsId: string) => ["feishu-project", wsId] as const,
  integration: (wsId: string) => [...feishuProjectKeys.all(wsId), "integration"] as const,
  issueStatuses: (wsId: string) => [...feishuProjectKeys.all(wsId), "issue-statuses"] as const,
  sync: (wsId: string) => [...feishuProjectKeys.all(wsId), "sync"] as const,
  fields: (wsId: string, workItemType: string) =>
    [...feishuProjectKeys.all(wsId), "fields", workItemType] as const,
  // Keyed by fieldKey + workItemType — switching the business-line field forces a refetch
  // of the option tree (since the tree IS that field's options, not space-wide biz lines).
  businessLines: (wsId: string, fieldKey: string, workItemType: string) =>
    [...feishuProjectKeys.all(wsId), "business-lines", workItemType, fieldKey] as const,
  routes: (wsId: string) => [...feishuProjectKeys.all(wsId), "routes"] as const,
};

export const feishuProjectIntegrationOptions = (wsId: string) =>
  queryOptions({
    queryKey: feishuProjectKeys.integration(wsId),
    queryFn: () => api.getFeishuProjectIntegration(wsId),
    enabled: !!wsId,
  });

export const feishuProjectIssueStatusesOptions = (wsId: string, enabled = true) =>
  queryOptions({
    queryKey: feishuProjectKeys.issueStatuses(wsId),
    queryFn: () => api.getFeishuProjectIssueStatuses(wsId),
    enabled: !!wsId && enabled,
  });

export const feishuProjectSyncOptions = (wsId: string, enabled = true) =>
  queryOptions({
    queryKey: feishuProjectKeys.sync(wsId),
    queryFn: () => api.getFeishuProjectSync(wsId),
    enabled: !!wsId && enabled,
  });

// Field list is only useful while the user is wiring up the integration — fetching it
// outside that flow burns plugin-token quota for no reason. Callers should pass
// `enabled: integration.enabled` so the query stays idle until the integration exists.
export const feishuProjectFieldsOptions = (wsId: string, workItemType = "issue", enabled = true) =>
  queryOptions({
    queryKey: feishuProjectKeys.fields(wsId, workItemType),
    queryFn: () => api.listFeishuProjectWorkItemFields(wsId, workItemType),
    enabled: !!wsId && enabled,
  });

// Tree comes from the selected field's options (NOT the space-wide /business/all),
// keyed on fieldKey so a different field choice refetches automatically. staleTime is
// Infinity because field options rarely change in a Meego space; invalidate explicitly
// (via queryClient) when the user clicks "refresh".
export const feishuProjectBusinessLinesOptions = (
  wsId: string,
  fieldKey: string,
  workItemType = "issue",
  enabled = true,
) =>
  queryOptions({
    queryKey: feishuProjectKeys.businessLines(wsId, fieldKey, workItemType),
    queryFn: () => api.listFeishuProjectBusinessLines(wsId, fieldKey, workItemType),
    enabled: !!wsId && !!fieldKey && enabled,
    staleTime: Infinity,
  });

export const feishuProjectRoutesOptions = (wsId: string, enabled = true) =>
  queryOptions({
    queryKey: feishuProjectKeys.routes(wsId),
    queryFn: () => api.listFeishuProjectRoutes(wsId),
    enabled: !!wsId && enabled,
  });

export function useReplaceFeishuProjectRoutes(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: ReplaceFeishuProjectRoutesRequest) => api.replaceFeishuProjectRoutes(wsId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: feishuProjectKeys.routes(wsId) });
    },
  });
}
