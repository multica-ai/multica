import { queryOptions } from "@tanstack/react-query";
import { api } from "../../api";

export const crAttemptKeys = {
  all: (wsId: string, issueId: string) => ["issues", wsId, issueId, "cr-attempts"] as const,
  signals: (wsId: string, issueId: string, attemptId: string) =>
    [...crAttemptKeys.all(wsId, issueId), attemptId, "signals"] as const,
};

export function crAttemptListOptions(wsId: string, issueId: string, refetchWhileOpen = false) {
  return queryOptions({
    queryKey: crAttemptKeys.all(wsId, issueId),
    queryFn: () => api.listCRAttempts(issueId),
    refetchInterval: refetchWhileOpen ? 15_000 : false,
  });
}

export function crSignalListOptions(wsId: string, issueId: string, attemptId: string) {
  return queryOptions({
    queryKey: crAttemptKeys.signals(wsId, issueId, attemptId),
    queryFn: () => api.listCRSignals(issueId, attemptId),
    enabled: Boolean(attemptId),
  });
}
