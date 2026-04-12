import { useQuery } from "@tanstack/react-query";
import type { User } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";

const SESSION_STALE_TIME = 60 * 1000;

export function hasStoredSessionToken(): boolean {
  if (typeof window === "undefined") {
    return false;
  }

  return Boolean(localStorage.getItem("multica_token"));
}

export function currentUserQueryOptions() {
  return {
    queryKey: queryKeys.session.me(),
    queryFn: () => api.getMe(),
    staleTime: SESSION_STALE_TIME,
  };
}

export function useCurrentUserQuery() {
  return useQuery<User>({
    ...currentUserQueryOptions(),
    enabled: hasStoredSessionToken(),
  });
}
