"use client";

import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { IssueStatus, UpdateWorkspaceColumnConfigRequest } from "../types";
import { api } from "../api";

export const columnConfigKeys = {
  all: (wsId: string) => ["column-configs", wsId] as const,
};

export function columnConfigListOptions(wsId: string) {
  return queryOptions({
    queryKey: columnConfigKeys.all(wsId),
    queryFn: () => api.listWorkspaceColumnConfigs(wsId),
  });
}

export function useColumnConfigs(wsId: string) {
  return useQuery(columnConfigListOptions(wsId));
}

export function useUpdateColumnConfig(wsId: string) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: ({
      status,
      ...data
    }: { status: IssueStatus } & UpdateWorkspaceColumnConfigRequest) =>
      api.updateWorkspaceColumnConfig(wsId, status, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: columnConfigKeys.all(wsId) });
    },
  });
}
