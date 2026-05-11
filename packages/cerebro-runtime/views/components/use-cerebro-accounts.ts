import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";

export const cerebroAccountKeys = {
  all: (wsId: string) => ["cerebro-accounts", wsId] as const,
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
