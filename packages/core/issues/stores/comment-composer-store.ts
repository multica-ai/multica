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
 * How images in the issue timeline's comments lay out: `thumbnails` keeps
 * them small (attached images in a row, body images height-capped) and opens
 * the full size in the viewer; `full` shows every image at its full size.
 */
export type CommentImageLayout = "thumbnails" | "full";

/**
 * Personal preferences for the issue-detail comment composer and timeline.
 *
 * `sticky` pins the bottom comment bar to the scroll viewport so it stays
 * reachable while reading a long timeline. `runningAgentReply` picks the
 * default for a reply to a running agent. `imageLayout` picks how comment
 * images lay out. All are personal habits (like theme), so they persist
 * globally via `defaultStorage` rather than per-workspace storage.
 */
interface CommentComposerStore {
  sticky: boolean;
  runningAgentReply: RunningAgentReply;
  imageLayout: CommentImageLayout;
  toggleSticky: () => void;
  setRunningAgentReply: (value: RunningAgentReply) => void;
  setImageLayout: (value: CommentImageLayout) => void;
}

export const useCommentComposerStore = create<CommentComposerStore>()(
  persist(
    (set) => ({
      sticky: true,
      runningAgentReply: "steer",
      imageLayout: "thumbnails",
      toggleSticky: () => set((s) => ({ sticky: !s.sticky })),
      setRunningAgentReply: (runningAgentReply) => set({ runningAgentReply }),
      setImageLayout: (imageLayout) => set({ imageLayout }),
    }),
    {
      name: "multica_comment_composer",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
