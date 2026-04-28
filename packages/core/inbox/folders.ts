import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  InboxFolder,
  InboxFolderItemType,
  InboxFolderMembership,
  InboxItem,
  ChatSession,
} from "../types";
import { inboxKeys } from "./queries";
import { chatKeys } from "../chat/queries";

export const inboxFolderKeys = {
  all: (wsId: string) => ["inbox-folders", wsId] as const,
  list: (wsId: string) => [...inboxFolderKeys.all(wsId), "list"] as const,
  memberships: (wsId: string) => [...inboxFolderKeys.all(wsId), "memberships"] as const,
};

export function inboxFolderListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxFolderKeys.list(wsId),
    queryFn: () => api.listInboxFolders(),
  });
}

export function inboxFolderMembershipsOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxFolderKeys.memberships(wsId),
    queryFn: () => api.listInboxFolderMemberships(),
  });
}

export function useCreateInboxFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (name: string) => api.createInboxFolder(name),
    onSuccess: (folder) => {
      qc.setQueryData<InboxFolder[]>(inboxFolderKeys.list(wsId), (old) =>
        old ? [...old, folder] : [folder],
      );
    },
  });
}

export function useRenameInboxFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) =>
      api.renameInboxFolder(id, name),
    onSuccess: (folder) => {
      qc.setQueryData<InboxFolder[]>(inboxFolderKeys.list(wsId), (old) =>
        old?.map((f) => (f.id === folder.id ? folder : f)),
      );
    },
  });
}

export function useDeleteInboxFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteInboxFolder(id),
    onSuccess: (_, id) => {
      qc.setQueryData<InboxFolder[]>(inboxFolderKeys.list(wsId), (old) =>
        old?.filter((f) => f.id !== id),
      );
      qc.setQueryData<InboxFolderMembership[]>(
        inboxFolderKeys.memberships(wsId),
        (old) => old?.filter((m) => m.folder_id !== id),
      );
      // Items previously in this folder fall back to the default inbox view.
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

export function useAddInboxFolderItem() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      folderId,
      itemType,
      itemId,
    }: {
      folderId: string;
      itemType: InboxFolderItemType;
      itemId: string;
    }) => api.addInboxFolderItem(folderId, itemType, itemId),
    onMutate: async ({ folderId, itemType, itemId }) => {
      await qc.cancelQueries({ queryKey: inboxFolderKeys.memberships(wsId) });
      const prev = qc.getQueryData<InboxFolderMembership[]>(
        inboxFolderKeys.memberships(wsId),
      );
      // Move semantics: drop other memberships for this item, add the new one.
      const next: InboxFolderMembership[] = (prev ?? []).filter(
        (m) => !(m.item_type === itemType && m.item_id === itemId),
      );
      next.push({
        folder_id: folderId,
        item_type: itemType,
        item_id: itemId,
        added_at: new Date().toISOString(),
      });
      qc.setQueryData(inboxFolderKeys.memberships(wsId), next);

      // Optimistically remove the item from the default inbox / chat list.
      if (itemType === "notification") {
        qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
          old?.filter((i) => i.id !== itemId),
        );
      } else {
        qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
          old?.filter((s) => s.id !== itemId),
        );
      }
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(inboxFolderKeys.memberships(wsId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxFolderKeys.memberships(wsId) });
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

export function useRemoveInboxFolderItem() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      folderId,
      itemType,
      itemId,
    }: {
      folderId: string;
      itemType: InboxFolderItemType;
      itemId: string;
    }) => api.removeInboxFolderItem(folderId, itemType, itemId),
    onMutate: async ({ folderId, itemType, itemId }) => {
      await qc.cancelQueries({ queryKey: inboxFolderKeys.memberships(wsId) });
      const prev = qc.getQueryData<InboxFolderMembership[]>(
        inboxFolderKeys.memberships(wsId),
      );
      qc.setQueryData<InboxFolderMembership[]>(
        inboxFolderKeys.memberships(wsId),
        (old) =>
          old?.filter(
            (m) =>
              !(
                m.folder_id === folderId &&
                m.item_type === itemType &&
                m.item_id === itemId
              ),
          ),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(inboxFolderKeys.memberships(wsId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxFolderKeys.memberships(wsId) });
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}
