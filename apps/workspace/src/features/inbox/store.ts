"use client";

import { useMemo } from "react";
import { create } from "zustand";
import type { InboxItem, IssueStatus } from "@/shared/types";
import { toast } from "sonner";
import { getAppQueryClient, queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";
import { inboxQueryOptions, useInboxItemsQuery } from "./queries";

/**
 * Deduplicate inbox items by issue_id (one entry per issue, Linear-style),
 * keep latest, sort by time DESC.
 * Memoized by reference — returns the same array if `items` hasn't changed.
 */
let _prevItems: InboxItem[] = [];
let _prevDeduped: InboxItem[] = [];

function deduplicateInboxItems(items: InboxItem[]): InboxItem[] {
  if (items === _prevItems) return _prevDeduped;
  _prevItems = items;

  const active = items.filter((i) => !i.archived);
  const groups = new Map<string, InboxItem[]>();
  active.forEach((item) => {
    const key = item.issue_id ?? item.id;
    const group = groups.get(key) ?? [];
    group.push(item);
    groups.set(key, group);
  });
  const merged: InboxItem[] = [];
  groups.forEach((group) => {
    const sorted = group.sort(
      (a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );
    if (sorted[0]) merged.push(sorted[0]);
  });
  _prevDeduped = merged.sort(
    (a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
  return _prevDeduped;
}

interface InboxState {
  items: InboxItem[];
  loading: boolean;
  fetch: () => Promise<void>;
  setItems: (items: InboxItem[]) => void;
  addItem: (item: InboxItem) => void;
  markRead: (id: string) => void;
  archive: (id: string) => void;
  markAllRead: () => void;
  archiveAll: () => void;
  archiveAllRead: () => void;
  updateIssueStatus: (issueId: string, status: IssueStatus) => void;
  dedupedItems: () => InboxItem[];
  unreadCount: () => number;
}

type InboxStoreSelector<T> = (state: InboxState) => T;

interface InboxStoreHook {
  <T>(selector: InboxStoreSelector<T>): T;
  getState: () => InboxState;
}

function getInboxSnapshot(): InboxState {
  const workspace = useWorkspaceStore.getState().workspace;
  const items = workspace
    ? getAppQueryClient().getQueryData<InboxItem[]>(queryKeys.inbox.all(workspace.id)) ?? []
    : [];

  return {
    items,
    loading: false,
    fetch: async () => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      try {
        await getAppQueryClient().fetchQuery(inboxQueryOptions(nextWorkspace.id));
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to load inbox");
      }
    },
    setItems: (nextItems) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData(queryKeys.inbox.all(nextWorkspace.id), nextItems);
    },
    addItem: (item) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<InboxItem[]>(queryKeys.inbox.all(nextWorkspace.id), (existing = []) =>
        existing.some((entry) => entry.id === item.id) ? existing : [item, ...existing],
      );
    },
    markRead: (id) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<InboxItem[]>(queryKeys.inbox.all(nextWorkspace.id), (existing = []) =>
        existing.map((item) => (item.id === id ? { ...item, read: true } : item)),
      );
    },
    archive: (id) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<InboxItem[]>(queryKeys.inbox.all(nextWorkspace.id), (existing = []) =>
        existing.map((item) => (item.id === id ? { ...item, archived: true } : item)),
      );
    },
    markAllRead: () => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<InboxItem[]>(queryKeys.inbox.all(nextWorkspace.id), (existing = []) =>
        existing.map((item) => (!item.archived ? { ...item, read: true } : item)),
      );
    },
    archiveAll: () => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<InboxItem[]>(queryKeys.inbox.all(nextWorkspace.id), (existing = []) =>
        existing.map((item) => (!item.archived ? { ...item, archived: true } : item)),
      );
    },
    archiveAllRead: () => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<InboxItem[]>(queryKeys.inbox.all(nextWorkspace.id), (existing = []) =>
        existing.map((item) => (item.read && !item.archived ? { ...item, archived: true } : item)),
      );
    },
    updateIssueStatus: (issueId, status) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<InboxItem[]>(queryKeys.inbox.all(nextWorkspace.id), (existing = []) =>
        existing.map((item) =>
          item.issue_id === issueId ? { ...item, issue_status: status } : item,
        ),
      );
    },
    dedupedItems: () => deduplicateInboxItems(items),
    unreadCount: () => deduplicateInboxItems(items).filter((item) => !item.read).length,
  };
}

export const useInboxStore = ((selector: InboxStoreSelector<unknown>) => {
  const inboxQuery = useInboxItemsQuery();

  const snapshot = useMemo<InboxState>(
    () => ({
      ...getInboxSnapshot(),
      items: inboxQuery.data ?? [],
      loading: inboxQuery.isPending,
      dedupedItems: () => deduplicateInboxItems(inboxQuery.data ?? []),
      unreadCount: () => deduplicateInboxItems(inboxQuery.data ?? []).filter((item) => !item.read).length,
    }),
    [inboxQuery.data, inboxQuery.isPending],
  );

  return selector(snapshot);
}) as InboxStoreHook;

useInboxStore.getState = getInboxSnapshot;
