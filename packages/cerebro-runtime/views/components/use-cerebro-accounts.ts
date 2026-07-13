import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { CerebroAccount, CreateCerebroAccountRequest } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";

export const cerebroAccountKeys = {
  all: (wsId: string) => ["cerebro-accounts", wsId] as const,
  usageHistory: (wsId: string, accountId: string) =>
    ["cerebro-accounts", wsId, "usage-history", accountId] as const,
};

export function cerebroAccountsListOptions(wsId: string) {
  return queryOptions({
    queryKey: cerebroAccountKeys.all(wsId),
    queryFn: () => api.listCerebroAccounts(wsId),
    enabled: Boolean(wsId),
  });
}

export function useCerebroAccountsList() {
  const wsId = useWorkspaceId();
  return useQuery(cerebroAccountsListOptions(wsId));
}

/**
 * Resolve a single account from the workspace-wide list. Returns `undefined`
 * while the list is loading, `null` if the id is unknown (or null), and the
 * account otherwise. We piggy-back on the existing list query so we don't
 * fan out one extra request per runtime-detail render.
 */
export function useCerebroAccount(
  accountId: string | null | undefined,
): { account: CerebroAccount | null; isLoading: boolean } {
  const { data: accounts, isLoading } = useCerebroAccountsList();
  if (isLoading) return { account: null, isLoading: true };
  if (!accountId || !accounts) return { account: null, isLoading: false };
  return {
    account: accounts.find((a) => a.id === accountId) ?? null,
    isLoading: false,
  };
}

/**
 * Hourly token-usage buckets for one account over the last 7 days
 * (FIR-3118). Backs the account detail page chart.
 */
export function useCerebroAccountUsageHistory(accountId: string) {
  const wsId = useWorkspaceId();
  return useQuery({
    queryKey: cerebroAccountKeys.usageHistory(wsId, accountId),
    queryFn: () => api.getCerebroAccountUsageHistory(wsId, accountId),
    enabled: Boolean(wsId) && Boolean(accountId),
  });
}

export function useCreateCerebroAccount() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateCerebroAccountRequest) =>
      api.createCerebroAccount(wsId, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: cerebroAccountKeys.all(wsId) });
    },
  });
}

export function useDeleteCerebroAccount() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteCerebroAccount(wsId, id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: cerebroAccountKeys.all(wsId) });
      const previous = qc.getQueryData<CerebroAccount[]>(
        cerebroAccountKeys.all(wsId),
      );
      if (previous) {
        qc.setQueryData<CerebroAccount[]>(
          cerebroAccountKeys.all(wsId),
          previous.filter((a) => a.id !== id),
        );
      }
      return { previous };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(cerebroAccountKeys.all(wsId), ctx.previous);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: cerebroAccountKeys.all(wsId) });
    },
  });
}
