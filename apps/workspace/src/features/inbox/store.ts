"use client";

import { create } from "zustand";
import type { InboxItem, IssueStatus } from "@/shared/types";
import { toast } from "sonner";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";

const logger = createLogger("inbox-store");

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

export const useInboxStore = create<InboxState>((set, get) => ({
  items: [],
  loading: true,

  fetch: async () => {
    logger.debug("fetch start");
    const isInitialLoad = get().items.length === 0;
    if (isInitialLoad) set({ loading: true });
    try {
      const data = await api.listInbox();
      logger.info("fetched", data.length, "items");
      set({ items: data, loading: false });
    } catch (err) {
      logger.error("fetch failed", err);
      toast.error("Failed to load inbox");
      if (isInitialLoad) set({ loading: false });
    }
  },

  setItems: (items) => set({ items }),
  addItem: (item) =>
    set((s) => ({
      items: s.items.some((i) => i.id === item.id)
        ? s.items
        : [item, ...s.items],
    })),
  markRead: (id) =>
    set((s) => ({
      items: s.items.map((i) => (i.id === id ? { ...i, read: true } : i)),
    })),
  archive: (id) =>
    set((s) => ({
      items: s.items.map((i) => (i.id === id ? { ...i, archived: true } : i)),
    })),
  markAllRead: () =>
    set((s) => ({
      items: s.items.map((i) => (!i.archived ? { ...i, read: true } : i)),
    })),
  archiveAll: () =>
    set((s) => ({
      items: s.items.map((i) => (!i.archived ? { ...i, archived: true } : i)),
    })),
  archiveAllRead: () =>
    set((s) => ({
      items: s.items.map((i) =>
        i.read && !i.archived ? { ...i, archived: true } : i
      ),
    })),
  updateIssueStatus: (issueId, status) =>
    set((s) => ({
      items: s.items.map((i) =>
        i.issue_id === issueId ? { ...i, issue_status: status } : i
      ),
    })),
  dedupedItems: () => deduplicateInboxItems(get().items),
  unreadCount: () =>
    get().dedupedItems().filter((i) => !i.read).length,
}));
