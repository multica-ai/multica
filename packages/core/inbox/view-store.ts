"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

export type InboxView =
  | "all"
  | "unread"
  | "mentioned"
  | "issues"
  | "channels"
  | "dms";

interface InboxViewState {
  view: InboxView;
  setView: (view: InboxView) => void;
}

/**
 * Workspace-scoped inbox view selector. Backs the "All ▾" dropdown in the
 * inbox header. Persisted per workspace because each workspace has its own
 * notion of "what matters right now".
 */
export const useInboxViewStore = create<InboxViewState>()(
  persist(
    (set) => ({
      view: "all",
      setView: (view) => set({ view }),
    }),
    {
      name: "multica_inbox_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useInboxViewStore.persist.rehydrate());

export const INBOX_VIEW_OPTIONS: { value: InboxView; label: string }[] = [
  { value: "all", label: "All" },
  { value: "unread", label: "Unread" },
  { value: "mentioned", label: "Mentioned" },
  { value: "issues", label: "Issues only" },
  { value: "channels", label: "Channels only" },
  { value: "dms", label: "DMs only" },
];
