import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { InboxItem } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";

function updateInboxItems(
  queryClient: ReturnType<typeof useQueryClient>,
  workspaceId: string | null,
  updater: (items: InboxItem[]) => InboxItem[],
) {
  if (!workspaceId) return;
  queryClient.setQueryData<InboxItem[]>(queryKeys.inbox.all(workspaceId), (existing = []) => updater(existing));
}

export function useInboxMutations() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id ?? null);

  const markReadMutation = useMutation({
    mutationFn: (itemId: string) => api.markInboxRead(itemId),
    onSuccess: (item) => {
      updateInboxItems(queryClient, workspaceId, (items) =>
        items.map((existing) => (existing.id === item.id ? item : existing)),
      );
    },
  });

  const archiveMutation = useMutation({
    mutationFn: (itemId: string) => api.archiveInbox(itemId),
    onSuccess: (item) => {
      updateInboxItems(queryClient, workspaceId, (items) =>
        items.map((existing) => (existing.id === item.id ? item : existing)),
      );
    },
  });

  const markAllReadMutation = useMutation({
    mutationFn: () => api.markAllInboxRead(),
    onSuccess: () => {
      updateInboxItems(queryClient, workspaceId, (items) =>
        items.map((item) => (!item.archived ? { ...item, read: true } : item)),
      );
    },
  });

  const archiveAllMutation = useMutation({
    mutationFn: () => api.archiveAllInbox(),
    onSuccess: () => {
      updateInboxItems(queryClient, workspaceId, (items) =>
        items.map((item) => (!item.archived ? { ...item, archived: true } : item)),
      );
    },
  });

  const archiveAllReadMutation = useMutation({
    mutationFn: () => api.archiveAllReadInbox(),
    onSuccess: () => {
      updateInboxItems(queryClient, workspaceId, (items) =>
        items.map((item) => (item.read && !item.archived ? { ...item, archived: true } : item)),
      );
    },
  });

  const archiveCompletedMutation = useMutation({
    mutationFn: () => api.archiveCompletedInbox(),
    onSuccess: async () => {
      if (!workspaceId) return;
      await queryClient.invalidateQueries({ queryKey: queryKeys.inbox.all(workspaceId) });
    },
  });

  return {
    markRead: (itemId: string) => markReadMutation.mutateAsync(itemId),
    archive: (itemId: string) => archiveMutation.mutateAsync(itemId),
    markAllRead: () => markAllReadMutation.mutateAsync(),
    archiveAll: () => archiveAllMutation.mutateAsync(),
    archiveAllRead: () => archiveAllReadMutation.mutateAsync(),
    archiveCompleted: () => archiveCompletedMutation.mutateAsync(),
    isMutating:
      markReadMutation.isPending ||
      archiveMutation.isPending ||
      markAllReadMutation.isPending ||
      archiveAllMutation.isPending ||
      archiveAllReadMutation.isPending ||
      archiveCompletedMutation.isPending,
  };
}
