import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const feishuProjectKeys = {
  all: (wsId: string) => ["feishu-project", wsId] as const,
  integration: (wsId: string) => [...feishuProjectKeys.all(wsId), "integration"] as const,
  issueStatuses: (wsId: string) => [...feishuProjectKeys.all(wsId), "issue-statuses"] as const,
  sync: (wsId: string) => [...feishuProjectKeys.all(wsId), "sync"] as const,
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
