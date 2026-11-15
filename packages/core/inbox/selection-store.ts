"use client";

import { create } from "zustand";

interface InboxSelectionState {
  lastResolvedKeyByWorkspace: Record<string, string>;
  rememberResolvedKey: (wsId: string, key: string) => void;
  /** The issue last opened from the Work panel's Inbox section. */
  sidebarIssueByWorkspace: Record<string, string | null>;
  rememberSidebarIssue: (wsId: string, issueId: string | null) => void;
  /**
   * The issue a notification row was last clicked for, with a counter that
   * moves on every click. The issue page replays its landing (the comment or
   * app request the link names) when the counter moves, which a click on the
   * row already open needs: the page neither remounts nor sees a new link.
   */
  landing: { issueId: string; token: number } | null;
  requestLanding: (issueId: string) => void;
}

// Keep only the last resolved pointer per workspace, for this client run.
// Notification data and its cache lifetime remain owned by TanStack Query.
export const useInboxSelectionStore = create<InboxSelectionState>()((set) => ({
  lastResolvedKeyByWorkspace: {},
  rememberResolvedKey: (wsId, key) =>
    set((state) => state.lastResolvedKeyByWorkspace[wsId] === key ? state : {
      lastResolvedKeyByWorkspace: { ...state.lastResolvedKeyByWorkspace, [wsId]: key },
    }),
  sidebarIssueByWorkspace: {},
  rememberSidebarIssue: (wsId, issueId) =>
    set((state) => (state.sidebarIssueByWorkspace[wsId] ?? null) === issueId ? state : {
      sidebarIssueByWorkspace: { ...state.sidebarIssueByWorkspace, [wsId]: issueId },
    }),
  landing: null,
  requestLanding: (issueId) =>
    set((state) => ({ landing: { issueId, token: (state.landing?.token ?? 0) + 1 } })),
}));
