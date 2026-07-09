/**
 * Workspace label list. Used by the label picker on issue detail.
 *
 * Workspace-scoped key — switching workspaces flips wsId and TanStack Query
 * picks up the new workspace's labels automatically.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const labelKeys = {
  all: (wsId: string | null) => ["labels", wsId] as const,
};

export const labelListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: labelKeys.all(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listLabels({ signal });
      return res.labels;
    },
    enabled: !!wsId,
  });
