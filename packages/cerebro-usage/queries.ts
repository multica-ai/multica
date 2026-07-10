import { queryOptions } from "@tanstack/react-query";
import { fetchSkillUsage } from "./api";
import { fetchUsageExplorer } from "./api";

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
