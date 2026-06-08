"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export interface CommentFoldSettings {
  enabled: boolean;
  /** Total replies in a thread before middle folding kicks in. */
  threshold: number;
  /** Replies kept at the top of a collapsed thread. */
  headCount: number;
  /** Replies kept at the bottom (near the composer). */
  tailCount: number;
}

export const COMMENT_FOLD_DEFAULTS: CommentFoldSettings = {
  enabled: true,
  threshold: 7,
  headCount: 2,
  tailCount: 2,
};

const MIN_THRESHOLD = 3;
const MAX_THRESHOLD = 50;
const MIN_WINDOW = 1;
const MAX_WINDOW = 20;

function clampInt(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, Math.round(value)));
}

/** Coerce persisted / user-edited values into a safe, usable shape. */
export function normalizeCommentFoldSettings(
  input: Partial<CommentFoldSettings> | null | undefined,
): CommentFoldSettings {
  const base = { ...COMMENT_FOLD_DEFAULTS, ...(input ?? {}) };

  const enabled = base.enabled !== false;
  const headCount = clampInt(base.headCount, MIN_WINDOW, MAX_WINDOW);
  const tailCount = clampInt(base.tailCount, MIN_WINDOW, MAX_WINDOW);
  let threshold = clampInt(base.threshold, MIN_THRESHOLD, MAX_THRESHOLD);

  if (headCount + tailCount >= threshold) {
    threshold = clampInt(headCount + tailCount + 1, MIN_THRESHOLD, MAX_THRESHOLD);
  }

  return { enabled, threshold, headCount, tailCount };
}

interface CommentFoldState {
  settings: CommentFoldSettings;
  patchSettings: (patch: Partial<CommentFoldSettings>) => void;
  resetSettings: () => void;
}

export const useCommentFoldStore = create<CommentFoldState>()(
  persist(
    (set) => ({
      settings: COMMENT_FOLD_DEFAULTS,
      patchSettings: (patch) =>
        set((state) => ({
          settings: normalizeCommentFoldSettings({ ...state.settings, ...patch }),
        })),
      resetSettings: () => set({ settings: COMMENT_FOLD_DEFAULTS }),
    }),
    {
      name: "multica_comment_fold",
      storage: createJSONStorage(() => defaultStorage),
      merge: (persisted, current) => ({
        ...current,
        settings: normalizeCommentFoldSettings(
          (persisted as Partial<CommentFoldState> | undefined)?.settings,
        ),
      }),
      partialize: (state) => ({ settings: state.settings }),
    },
  ),
);
