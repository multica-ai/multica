import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * How an issue reference inside content (issue descriptions, comments, chat
 * markdown, and the editor) renders.
 *
 * `full` shows status icon + identifier + title and is the default — it is the
 * historical rendering, so nobody's reading experience changes until they opt
 * in. `compact` drops the title and `plain` drops the chip entirely, both
 * revealing the title on hover instead. Reference-dense prose is much easier to
 * read in the narrower modes.
 *
 * This is a personal reading-ergonomics preference (closer to theme than to
 * workspace policy), so it persists globally via `defaultStorage` rather than
 * per-workspace storage.
 */
export type IssueMentionMode = "plain" | "compact" | "full";

interface IssueMentionDisplayStore {
  mode: IssueMentionMode;
  setMode: (mode: IssueMentionMode) => void;
}

export const useIssueMentionDisplayStore = create<IssueMentionDisplayStore>()(
  persist(
    (set) => ({
      mode: "full",
      setMode: (mode) => set({ mode }),
    }),
    {
      name: "multica_issue_mention_display",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
