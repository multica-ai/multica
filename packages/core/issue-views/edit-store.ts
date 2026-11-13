"use client";

import { create } from "zustand";

interface IssueViewEditState {
  viewId: string | null;
  start: (viewId: string) => void;
  stop: () => void;
}

/**
 * A saved view asked to be edited from outside its page, e.g. the sidebar.
 * The page header that shows the view opens its filter menu and clears it.
 */
export const useIssueViewEditStore = create<IssueViewEditState>((set) => ({
  viewId: null,
  start: (viewId) => set({ viewId }),
  stop: () => set({ viewId: null }),
}));
