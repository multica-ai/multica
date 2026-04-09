"use client";

import { create } from "zustand";

interface IssueClientState {
  activeIssueId: string | null;
  setActiveIssue: (id: string | null) => void;
}

export const useIssueStore = create<IssueClientState>((set) => ({
  activeIssueId: null,
  setActiveIssue: (id) => set({ activeIssueId: id }),
}));
