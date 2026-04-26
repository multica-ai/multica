import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueLabel } from "../types";

export const labelKeys = {
  all: (wsId: string) => ["labels", wsId] as const,
  list: (wsId: string) => [...labelKeys.all(wsId), "list"] as const,
};

export function labelListOptions(wsId: string) {
  return queryOptions({
    queryKey: labelKeys.list(wsId),
    queryFn: () => api.listLabels(wsId),
    // Server returns labels ordered by name asc; keep that ordering on the
    // client so UI is stable across refetches.
    select: (data: IssueLabel[]) => data,
  });
}
