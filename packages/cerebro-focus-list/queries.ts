"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";

export const focusListKeys = {
  all: (wsId: string) => ["focus-list", wsId] as const,
};

export function focusListOptions(wsId: string) {
  return {
    queryKey: focusListKeys.all(wsId),
    queryFn: () => api.listFocusListItems(),
    staleTime: 30_000,
  };
}

export function useFocusList() {
  const wsId = useWorkspaceId();
  return useQuery(focusListOptions(wsId));
}
