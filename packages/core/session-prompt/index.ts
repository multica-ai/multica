"use client";

import { create } from "zustand";

interface SessionPromptStore {
  /** A prompt that asks something of the user has been shown this app session. */
  prompted: boolean;
  markPrompted: () => void;
}

/**
 * The one interruption an app session allows, shared by the source survey,
 * the GitHub star prompt and the What's new deck so a user who just dealt with
 * one never meets another.
 */
export const useSessionPromptStore = create<SessionPromptStore>((set) => ({
  prompted: false,
  markPrompted: () => set({ prompted: true }),
}));
