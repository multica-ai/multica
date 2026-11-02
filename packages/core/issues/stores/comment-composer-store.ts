import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * What a reply does by default when it reaches a running agent that would
 * otherwise take it in its current turn: `steer` adds it to that turn,
 * `after_run` starts a new run once the current one ends. Either way the
 * composer's recipient chip can change it for a single message.
 */
export type RunningAgentReply = "steer" | "after_run";

/**
 * Personal preferences for the issue-detail comment composer.
 *
 * `sticky` pins the bottom comment bar to the scroll viewport so it stays
 * reachable while reading a long timeline. `runningAgentReply` picks the
 * default for a reply to a running agent. Both are personal habits (like
 * theme), so they persist globally via `defaultStorage` rather than
 * per-workspace storage.
 */
interface CommentComposerStore {
  sticky: boolean;
  runningAgentReply: RunningAgentReply;
  toggleSticky: () => void;
  setRunningAgentReply: (value: RunningAgentReply) => void;
}

export const useCommentComposerStore = create<CommentComposerStore>()(
  persist(
    (set) => ({
      sticky: true,
      runningAgentReply: "steer",
      toggleSticky: () => set((s) => ({ sticky: !s.sticky })),
      setRunningAgentReply: (runningAgentReply) => set({ runningAgentReply }),
    }),
    {
      name: "multica_comment_composer",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
