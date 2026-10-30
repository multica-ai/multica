import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export const ISSUE_BROWSER_MIN_WIDTH = 320;
export const ISSUE_BROWSER_DEFAULT_WIDTH = 480;

interface IssueBrowserState {
  /** Issues whose browser panel is open. In-memory: guest pages die with the app. */
  openIssueIds: Record<string, true>;
  /** Panel width in px, shared by all issues and saved on this device. */
  width: number;
  toggle: (issueId: string) => void;
  close: (issueId: string) => void;
  setWidth: (width: number) => void;
}

export const useIssueBrowserStore = create<IssueBrowserState>()(
  persist(
    (set) => ({
      openIssueIds: {},
      width: ISSUE_BROWSER_DEFAULT_WIDTH,
      toggle: (issueId) =>
        set((s) => {
          const next = { ...s.openIssueIds };
          if (next[issueId]) delete next[issueId];
          else next[issueId] = true;
          return { openIssueIds: next };
        }),
      close: (issueId) =>
        set((s) => {
          if (!s.openIssueIds[issueId]) return s;
          const next = { ...s.openIssueIds };
          delete next[issueId];
          return { openIssueIds: next };
        }),
      setWidth: (width) =>
        set({ width: Math.max(ISSUE_BROWSER_MIN_WIDTH, Math.round(width)) }),
    }),
    {
      name: "multica_issue_browser",
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ width }) => ({ width }),
      merge: (persisted, current) => {
        const width =
          persisted && typeof persisted === "object" && "width" in persisted
            ? Number(persisted.width)
            : NaN;
        return {
          ...current,
          width: Number.isFinite(width)
            ? Math.max(ISSUE_BROWSER_MIN_WIDTH, width)
            : current.width,
        };
      },
    },
  ),
);
