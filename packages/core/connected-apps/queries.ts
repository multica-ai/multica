import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { AppAccountScope } from "../types";

/** Query keys for Connected Apps (MUL-8516). Everything sits under the
 * workspace id: team accounts, activity, and requests are workspace data. */
export const connectedAppsKeys = {
  all: (wsId: string) => ["connected-apps", wsId] as const,
  overview: (wsId: string) => [...connectedAppsKeys.all(wsId), "overview"] as const,
  catalog: (wsId: string) => [...connectedAppsKeys.all(wsId), "catalog"] as const,
  issueCalls: (wsId: string, issueId: string) =>
    [...connectedAppsKeys.all(wsId), "calls", issueId] as const,
  calls: (wsId: string) => [...connectedAppsKeys.all(wsId), "calls"] as const,
  activity: (wsId: string, slug: string, scope: AppAccountScope) =>
    [...connectedAppsKeys.all(wsId), "activity", slug, scope] as const,
  impact: (wsId: string, slug: string) => [...connectedAppsKeys.all(wsId), "impact", slug] as const,
  agentApps: (wsId: string, agentId: string) =>
    [...connectedAppsKeys.all(wsId), "agent", agentId] as const,
  requests: (wsId: string) => [...connectedAppsKeys.all(wsId), "requests"] as const,
  issueRequests: (wsId: string, issueId: string) =>
    [...connectedAppsKeys.requests(wsId), "issue", issueId] as const,
  chatRequests: (wsId: string, sessionId: string) =>
    [...connectedAppsKeys.requests(wsId), "chat", sessionId] as const,
};

export const appsOverviewOptions = (wsId: string) =>
  queryOptions({
    queryKey: connectedAppsKeys.overview(wsId),
    queryFn: () => api.getAppsOverview(),
  });

/** The catalog changes rarely; keep it for a few minutes. */
export const appCatalogOptions = (wsId: string) =>
  queryOptions({
    queryKey: connectedAppsKeys.catalog(wsId),
    queryFn: () => api.listAppCatalog(),
    staleTime: 5 * 60 * 1000,
  });

/** One fetch per issue backs both the comment chips and the sidebar. */
export const issueAppCallsOptions = (wsId: string, issueId: string) =>
  queryOptions({
    queryKey: connectedAppsKeys.issueCalls(wsId, issueId),
    queryFn: () => api.listIssueAppCalls(issueId).then((r) => r.calls),
  });

export const appActivityOptions = (wsId: string, slug: string, scope: AppAccountScope) =>
  queryOptions({
    queryKey: connectedAppsKeys.activity(wsId, slug, scope),
    queryFn: () => api.listAppActivity(slug, scope).then((r) => r.calls),
  });

export const teamAppImpactOptions = (wsId: string, slug: string) =>
  queryOptions({
    queryKey: connectedAppsKeys.impact(wsId, slug),
    queryFn: () => api.getTeamAppImpact(slug),
  });

export const agentAppsOptions = (wsId: string, agentId: string) =>
  queryOptions({
    queryKey: connectedAppsKeys.agentApps(wsId, agentId),
    queryFn: () => api.listAgentApps(agentId).then((r) => r.apps),
  });

export const issueAppRequestsOptions = (wsId: string, issueId: string) =>
  queryOptions({
    queryKey: connectedAppsKeys.issueRequests(wsId, issueId),
    queryFn: () => api.listAppRequests({ issueId }).then((r) => r.requests),
  });

export const chatAppRequestsOptions = (wsId: string, sessionId: string) =>
  queryOptions({
    queryKey: connectedAppsKeys.chatRequests(wsId, sessionId),
    queryFn: () => api.listAppRequests({ chatSessionId: sessionId }).then((r) => r.requests),
  });
