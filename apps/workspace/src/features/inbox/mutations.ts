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

  const removeTriagedItem = (itemId: string) => {
    updateInboxItems(queryClient, workspaceId, (items) => items.filter((item) => item.id !== itemId));
  };

  const handleMutation = useMutation({
    mutationFn: (itemId: string) => api.handleInbox(itemId),
    onSuccess: (item) => {
      removeTriagedItem(item.id);
    },
  });

  const dismissMutation = useMutation({
    mutationFn: (itemId: string) => api.dismissInbox(itemId),
    onSuccess: (item) => {
      removeTriagedItem(item.id);
    },
  });

  const snoozeMutation = useMutation({
    mutationFn: ({ itemId, snoozedUntil }: { itemId: string; snoozedUntil: string }) =>
      api.snoozeInbox(itemId, snoozedUntil),
    onSuccess: (item) => {
      removeTriagedItem(item.id);
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

  const handleCompletedMutation = useMutation({
    mutationFn: () => api.handleCompletedInbox(),
    onSuccess: async () => {
      if (!workspaceId) return;
      await queryClient.invalidateQueries({ queryKey: queryKeys.inbox.all(workspaceId) });
    },
  });

  const batchHandleMutation = useMutation({
    mutationFn: () => api.batchHandleInbox(),
    onSuccess: async () => {
      if (!workspaceId) return;
      await queryClient.invalidateQueries({ queryKey: queryKeys.inbox.all(workspaceId) });
    },
  });

  const batchDismissMutation = useMutation({
    mutationFn: () => api.batchDismissInbox(),
    onSuccess: async () => {
      if (!workspaceId) return;
      await queryClient.invalidateQueries({ queryKey: queryKeys.inbox.all(workspaceId) });
    },
  });

  const batchSnoozeMutation = useMutation({
    mutationFn: (snoozedUntil: string) => api.batchSnoozeInbox(snoozedUntil),
    onSuccess: async () => {
      if (!workspaceId) return;
      await queryClient.invalidateQueries({ queryKey: queryKeys.inbox.all(workspaceId) });
    },
  });

  return {
    markRead: (itemId: string) => markReadMutation.mutateAsync(itemId),
    archive: (itemId: string) => archiveMutation.mutateAsync(itemId),
    handle: (itemId: string) => handleMutation.mutateAsync(itemId),
    dismiss: (itemId: string) => dismissMutation.mutateAsync(itemId),
    snooze: (itemId: string, snoozedUntil: string) => snoozeMutation.mutateAsync({ itemId, snoozedUntil }),
    markAllRead: () => markAllReadMutation.mutateAsync(),
    archiveAll: () => archiveAllMutation.mutateAsync(),
    archiveAllRead: () => archiveAllReadMutation.mutateAsync(),
    handleCompleted: () => handleCompletedMutation.mutateAsync(),
    batchHandle: () => batchHandleMutation.mutateAsync(),
    batchDismiss: () => batchDismissMutation.mutateAsync(),
    batchSnooze: (snoozedUntil: string) => batchSnoozeMutation.mutateAsync(snoozedUntil),
    isMutating:
      markReadMutation.isPending ||
      archiveMutation.isPending ||
      handleMutation.isPending ||
      dismissMutation.isPending ||
      snoozeMutation.isPending ||
      markAllReadMutation.isPending ||
      archiveAllMutation.isPending ||
      archiveAllReadMutation.isPending ||
      handleCompletedMutation.isPending ||
      batchHandleMutation.isPending ||
      batchDismissMutation.isPending ||
      batchSnoozeMutation.isPending,
  };
}
