"use client";

import { create } from "zustand";

/** Cards that slide into the bottom-right corner, which holds one at a time. */
export type CornerCard = "whats-new" | "star";

interface SessionPromptStore {
  /**
   * A prompt that asks something of the user has been shown this app session.
   * Shared by the source survey and the GitHub star prompt; the What's new
   * deck only informs, so it neither spends nor respects this.
   */
  prompted: boolean;
  /** The What's new deck has been dealt this app session. */
  deckDealt: boolean;
  /** The source survey is open, or has yet to decide whether to open. */
  surveyPending: boolean;
  corner: CornerCard | null;
  markPrompted: () => void;
  markDeckDealt: () => void;
  setSurveyPending: (pending: boolean) => void;
  /** True when the corner is now `card`'s, false when another card holds it. */
  claimCorner: (card: CornerCard) => boolean;
  releaseCorner: (card: CornerCard) => void;
  /** Forgets what this session has shown, as a reload would; what is on screen keeps its hold. */
  reset: () => void;
}

/**
 * Keeps the app's own prompts from piling onto each other within an app
 * session: who has interrupted already, whether the survey is about to, and
 * which card holds the corner.
 */
export const useSessionPromptStore = create<SessionPromptStore>((set, get) => ({
  prompted: false,
  deckDealt: false,
  surveyPending: false,
  corner: null,
  markPrompted: () => set({ prompted: true }),
  markDeckDealt: () => set({ deckDealt: true }),
  setSurveyPending: (pending) => set({ surveyPending: pending }),
  claimCorner: (card) => {
    const { corner } = get();
    if (corner !== null && corner !== card) return false;
    if (corner !== card) set({ corner: card });
    return true;
  },
  releaseCorner: (card) => {
    if (get().corner === card) set({ corner: null });
  },
  reset: () => set({ prompted: false, deckDealt: false }),
}));

/** A prompt the dev toolbar asked to show, bypassing its gates. */
export type DevPromptKind = "whats-new" | "whats-new-check" | "star" | "survey";

interface DevPromptRequest {
  kind: DevPromptKind;
  id: number;
}

interface DevPromptStore {
  request: DevPromptRequest | null;
  trigger: (kind: DevPromptKind) => void;
  /** Takes the pending request when it is of `kind`, so a remount does not replay it. */
  take: (kind: DevPromptKind) => boolean;
}

let nextRequestId = 0;

/** Dev toolbar → prompt components. Nothing triggers it outside development. */
export const useDevPromptStore = create<DevPromptStore>((set, get) => ({
  request: null,
  trigger: (kind) => set({ request: { kind, id: ++nextRequestId } }),
  take: (kind) => {
    if (get().request?.kind !== kind) return false;
    set({ request: null });
    return true;
  },
}));
