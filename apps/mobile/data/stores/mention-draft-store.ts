/**
 * Cross-route draft store for the comment composer's @mention chips.
 *
 * The mention picker route (`app/(app)/[workspace]/issue/[id]/picker/mention.tsx`)
 * lives in its own formSheet and cannot share callbacks with the composer
 * that opened it. Same pattern as how label / assignee pickers
 * communicate with their issue-detail screen — except those write straight
 * to a mutation (durable state), while this store holds purely client-side
 * draft state until the composer either sends or unmounts.
 *
 * Scope is intentionally narrow: ONE slot (mentions). Attachments stay as
 * local composer state because no route outside the composer needs to
 * touch them.
 *
 * Lifecycle: composer's `useEffect` cleanup calls `clear()` so navigating
 * to another issue starts with an empty mention array. Send success also
 * clears.
 */
import { create } from "zustand";

export type MentionTargetType = "member" | "agent" | "squad" | "all" | "issue";

export interface MentionChipDraft {
  type: MentionTargetType;
  /** UUID for member / agent / squad / issue; literal "all" for @all. */
  id: string;
  /** Display name without leading `@`. For type "issue" this stores the
   *  human identifier (e.g. "MUL-123"). */
  name: string;
}

function sameMention(
  a: MentionChipDraft,
  b: { type: MentionTargetType; id: string },
) {
  return a.type === b.type && a.id === b.id;
}

interface State {
  mentions: MentionChipDraft[];
  /** Add or remove by (type, id). Picker uses this — selecting an
   *  already-selected row removes it (label-picker idiom). */
  toggle: (mention: MentionChipDraft) => void;
  remove: (type: MentionTargetType, id: string) => void;
  clear: () => void;
}

export const useMentionDraftStore = create<State>((set) => ({
  mentions: [],
  toggle: (mention) =>
    set((s) => {
      const existing = s.mentions.some((m) => sameMention(m, mention));
      if (existing) {
        return {
          mentions: s.mentions.filter((m) => !sameMention(m, mention)),
        };
      }
      return { mentions: [...s.mentions, mention] };
    }),
  remove: (type, id) =>
    set((s) => ({
      mentions: s.mentions.filter((m) => !sameMention(m, { type, id })),
    })),
  clear: () => set({ mentions: [] }),
}));
