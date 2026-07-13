import { queryOptions } from "@tanstack/react-query";
import { fetchSkillUsage } from "./api";
import { fetchUsageExplorer } from "./api";
import { fetchAnalyticsCatalog, fetchAnalyticsQuery, fetchAnalyticsVisuals, type AnalyticsQuery } from "./api";

export const analyticsCatalogOptions = (workspaceId: string) =>
  queryOptions({
    queryKey: ["analytics", workspaceId, "catalog"] as const,
    queryFn: fetchAnalyticsCatalog,
    enabled: workspaceId.length > 0,
    staleTime: 5 * 60_000,
  });

export const analyticsQueryOptions = (workspaceId: string, query: AnalyticsQuery) =>
  queryOptions({
    queryKey: ["analytics", workspaceId, "query", query] as const,
    queryFn: () => fetchAnalyticsQuery(query),
    enabled: workspaceId.length > 0,
    staleTime: 60_000,
  });

export const analyticsVisualsOptions = (workspaceId: string) =>
  queryOptions({
    queryKey: ["analytics", workspaceId, "visuals"] as const,
    queryFn: fetchAnalyticsVisuals,
    enabled: workspaceId.length > 0,
  });

export const usageExplorerOptions = (workspaceId:string, query:string) => queryOptions({ queryKey:["usage",workspaceId,"explorer",query] as const, queryFn:()=>fetchUsageExplorer(query), enabled:workspaceId.length>0, staleTime:60_000 });

export const skillUsageOptions = (
  workspaceId: string,
  days: number,
  projectId: string | null,
  include: string[] = [],
  exclude: string[] = [],
) =>
  queryOptions({
    queryKey: ["usage", workspaceId, "skills", days, projectId, include, exclude] as const,
    queryFn: () => fetchSkillUsage(days, projectId, include, exclude),
    enabled: workspaceId.length > 0,
    staleTime: 60_000,
  });
