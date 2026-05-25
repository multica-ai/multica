"use client";

import { create } from "zustand";

export interface ActiveIssueContext {
  issueId: string;
  identifier: string;
  projectId: string | null;
}

interface ActiveIssueContextState {
  current: ActiveIssueContext | null;
  setCurrent: (context: ActiveIssueContext) => void;
  clearCurrent: (issueId: string) => void;
}

export const useActiveIssueContextStore = create<ActiveIssueContextState>()(
  (set) => ({
    current: null,
    setCurrent: (context) => set({ current: context }),
    clearCurrent: (issueId) =>
      set((state) =>
        state.current?.issueId === issueId ? { current: null } : state,
      ),
  }),
);
