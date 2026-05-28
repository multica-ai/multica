import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { DAGAnalysisResponse } from "../types";

export const dagKeys = {
  all: (wsId: string) => ["dag", wsId] as const,
  analysis: (wsId: string) => [...dagKeys.all(wsId), "analysis"] as const,
};

export const dagAnalysisOptions = (wsId: string) =>
  queryOptions({
    queryKey: dagKeys.analysis(wsId),
    queryFn: (): Promise<DAGAnalysisResponse> => api.getDAGAnalysis(),
    staleTime: 30_000,
  });
