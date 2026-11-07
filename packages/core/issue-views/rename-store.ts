"use client";

import { create } from "zustand";

/** Where an inline rename is open: one place at a time, never two inputs. */
export type IssueViewRenameSurface = "sidebar" | "header";

interface IssueViewRenameState {
  target: { viewId: string; surface: IssueViewRenameSurface } | null;
  start: (viewId: string, surface: IssueViewRenameSurface) => void;
  stop: () => void;
}

/**
 * The view whose name is being edited inline. Creating or duplicating a view
 * opens it here right away so the user names it where it appears.
 */
export const useIssueViewRenameStore = create<IssueViewRenameState>((set) => ({
  target: null,
  start: (viewId, surface) => set({ target: { viewId, surface } }),
  stop: () => set({ target: null }),
}));
