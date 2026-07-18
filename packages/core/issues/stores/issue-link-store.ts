import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * How issue mention links inside content (issue descriptions, comments,
 * chat markdown) open on plain click.
 *
 * `openInNewTab: true` (default) opens the linked issue in a new tab — a
 * browser tab on web, an app tab on desktop — so the reader keeps their
 * place in the issue they came from. `false` navigates in place. It's a
 * personal reading-ergonomics preference (like theme), so it persists
 * globally via `defaultStorage` rather than per-workspace storage.
 */
interface IssueLinkStore {
  openInNewTab: boolean;
  setOpenInNewTab: (open: boolean) => void;
}

export const useIssueLinkStore = create<IssueLinkStore>()(
  persist(
    (set) => ({
      openInNewTab: true,
      setOpenInNewTab: (open) => set({ openInNewTab: open }),
    }),
    {
      name: "multica_issue_link",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
