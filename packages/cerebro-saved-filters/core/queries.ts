import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import {
  createSavedFilter,
  deleteSavedFilter,
  listSavedFilters,
  updateSavedFilter,
} from "./api";
import type { CreateSavedFilterInput, SavedFilter, UpdateSavedFilterInput } from "./types";

// Query keys scoped by workspace so switching workspaces never leaks cached
// cross-workspace data (CLAUDE.md → workspace-scoped queries). Surface is part
// of the key so a per-surface list and the all-surfaces list cache separately.
export const savedFiltersKeys = {
  all: (wsId: string) => ["cerebro", "saved-filters", wsId] as const,
  list: (wsId: string, surface?: string) =>
    [...savedFiltersKeys.all(wsId), surface ?? "*"] as const,
};

export function savedFiltersOptions(wsId: string, surface?: string) {
  return queryOptions({
    queryKey: savedFiltersKeys.list(wsId, surface),
    queryFn: () => listSavedFilters(surface),
    enabled: !!wsId,
    staleTime: 30 * 1000,
  });
}

export function useSavedFilters(wsId: string, surface?: string) {
  return useQuery(savedFiltersOptions(wsId, surface));
}

export function useCreateSavedFilter(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateSavedFilterInput) => createSavedFilter(input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: savedFiltersKeys.all(wsId) });
    },
  });
}

export function useUpdateSavedFilter(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: string; input: UpdateSavedFilterInput }) =>
      updateSavedFilter(args.id, args.input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: savedFiltersKeys.all(wsId) });
    },
  });
}

export function useDeleteSavedFilter(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteSavedFilter(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: savedFiltersKeys.all(wsId) });
    },
  });
}

export type { SavedFilter };
