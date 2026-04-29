import { useQuery } from "@tanstack/react-query";
import type { InboxItem } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { hasStoredSessionToken } from "@/features/auth/queries";
import { useWorkspaceStore } from "@/features/workspace";

const INBOX_STALE_TIME = 30 * 1000;

export function inboxQueryOptions(workspaceId: string) {
  return {
    queryKey: queryKeys.inbox.all(workspaceId),
    queryFn: () => api.listInbox(),
    staleTime: INBOX_STALE_TIME,
  };
}

export function useInboxItemsQuery() {
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useQuery<InboxItem[]>({
    ...(workspaceId
      ? inboxQueryOptions(workspaceId)
      : {
          queryKey: queryKeys.inbox.all("__no_workspace__"),
          queryFn: async () => [] as InboxItem[],
          staleTime: INBOX_STALE_TIME,
        }),
    enabled: Boolean(workspaceId) && hasStoredSessionToken(),
  });
}
