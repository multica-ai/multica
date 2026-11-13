"use client";

import { create } from "zustand";

interface InboxSelectionState {
  lastResolvedKeyByWorkspace: Record<string, string>;
  rememberResolvedKey: (wsId: string, key: string) => void;
}

// Keep only the last resolved pointer per workspace, for this client run.
// Notification data and its cache lifetime remain owned by TanStack Query.
export const useInboxSelectionStore = create<InboxSelectionState>()((set) => ({
  lastResolvedKeyByWorkspace: {},
  rememberResolvedKey: (wsId, key) =>
    set((state) => state.lastResolvedKeyByWorkspace[wsId] === key ? state : {
      lastResolvedKeyByWorkspace: { ...state.lastResolvedKeyByWorkspace, [wsId]: key },
    }),
}));
