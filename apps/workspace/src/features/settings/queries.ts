import { useQuery } from "@tanstack/react-query";
import type { NotificationPreference, PersonalAccessToken } from "@/shared/types";
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

export function notificationPreferencesQueryOptions() {
  return {
    queryKey: queryKeys.settings.notificationPreferences(),
    queryFn: () => api.getNotificationPreferences(),
    staleTime: 60 * 1000,
  };
}

export function useNotificationPreferencesQuery() {
  return useQuery<NotificationPreference>({
    ...notificationPreferencesQueryOptions(),
    enabled: hasStoredSessionToken(),
  });
}
