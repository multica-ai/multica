import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * Display preferences for the issue-detail comment composer.
 *
 * `sticky` pins the bottom comment bar to the scroll viewport so it stays
 * reachable while reading a long timeline. It's a personal reading-ergonomics
 * preference (like theme), so it persists globally via `defaultStorage`
 * rather than per-workspace storage.
 */
interface CommentComposerStore {
  sticky: boolean;
  toggleSticky: () => void;
}

export const useCommentComposerStore = create<CommentComposerStore>()(
  persist(
    (set) => ({
      sticky: true,
      toggleSticky: () => set((s) => ({ sticky: !s.sticky })),
    }),
    {
      name: "multica_comment_composer",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
