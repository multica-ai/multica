"use client";

import { create } from "zustand";

/**
 * Threads picked for a share image. Selection lives on one issue at a time and
 * is not persisted: leaving the issue or finishing the share ends it.
 */
interface CommentShareState {
  /** Issue whose timeline is in selection mode; null when inactive. */
  issueId: string | null;
  /** Selected thread root ids, in the order they were picked. */
  selected: string[];
  /** Enter selection mode on `issueId`, starting with `rootId` when given. */
  start: (issueId: string, rootId?: string) => void;
  toggle: (rootId: string) => void;
  clear: () => void;
}

export const useCommentShareStore = create<CommentShareState>()((set) => ({
  issueId: null,
  selected: [],
  start: (issueId, rootId) =>
    set((s) => {
      const selected = s.issueId === issueId ? s.selected : [];
      return {
        issueId,
        selected: rootId && !selected.includes(rootId) ? [...selected, rootId] : selected,
      };
    }),
  toggle: (rootId) =>
    set((s) => ({
      selected: s.selected.includes(rootId)
        ? s.selected.filter((id) => id !== rootId)
        : [...s.selected, rootId],
    })),
  clear: () => set({ issueId: null, selected: [] }),
}));
