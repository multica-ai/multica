import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";

import {
  fetchIssueDateTimes,
  fetchWorkspaceDateTimeSettings,
  saveIssueDateTimes,
  saveWorkspaceDateTimeSettings,
} from "./api";
import type { AgentStartKind, IssueDateTimes } from "./types";

// Query keys scoped by workspace so switching workspaces never leaks cached
// cross-workspace data (per CLAUDE.md → workspace-scoped queries).
export const issueDateTimesKeys = {
  all: (wsId: string) => ["cerebro", "issue-date-times", wsId] as const,
  issue: (wsId: string, issueId: string) =>
    [...issueDateTimesKeys.all(wsId), issueId] as const,
  settings: (wsId: string) =>
    ["cerebro", "issue-date-times-settings", wsId] as const,
};

export function issueDateTimesOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueDateTimesKeys.issue(wsId, issueId),
    queryFn: () => fetchIssueDateTimes(issueId),
    enabled: !!wsId && !!issueId,
    staleTime: 30 * 1000,
  });
}

export function workspaceDateTimeSettingsOptions(wsId: string) {
  return queryOptions({
    queryKey: issueDateTimesKeys.settings(wsId),
    queryFn: () => fetchWorkspaceDateTimeSettings(wsId),
    enabled: !!wsId,
    staleTime: 60 * 1000,
  });
}

export function useSetIssueDateTimes(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: IssueDateTimes) => saveIssueDateTimes(issueId, payload),
    onSuccess: (data) => {
      qc.setQueryData(issueDateTimesKeys.issue(wsId, issueId), data);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: issueDateTimesKeys.issue(wsId, issueId) });
    },
  });
}

export function useSetWorkspaceDateTimeSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (kind: AgentStartKind) =>
      saveWorkspaceDateTimeSettings(wsId, kind),
    onSuccess: (data) => {
      qc.setQueryData(issueDateTimesKeys.settings(wsId), data);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: issueDateTimesKeys.settings(wsId) });
    },
  });
}
