import { useQuery } from "@tanstack/react-query";
import type { PersonalAccessToken } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { hasStoredSessionToken } from "@/features/auth/queries";

export function personalAccessTokensQueryOptions() {
  return {
    queryKey: queryKeys.settings.tokens(),
    queryFn: () => api.listPersonalAccessTokens(),
    staleTime: 30 * 1000,
  };
}

export function usePersonalAccessTokensQuery() {
  return useQuery<PersonalAccessToken[]>({
    ...personalAccessTokensQueryOptions(),
    enabled: hasStoredSessionToken(),
  });
}
