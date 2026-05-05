import { create } from "zustand";
import type { TimeEntry } from "@/shared/types";

interface TimeTrackingState {
  /** The currently running timer entry, or null if no timer is active. */
  currentEntry: TimeEntry | null;
}

interface TimeTrackingActions {
  setCurrentEntry: (entry: TimeEntry | null) => void;
}

/**
 * Global store for the currently running time entry.
 * Shared between GlobalTimerWidget and IssueTimerSection so both
 * surfaces stay in sync without passing props.
 */
export const useTimeTrackingStore = create<TimeTrackingState & TimeTrackingActions>((set) => ({
  currentEntry: null,
  setCurrentEntry: (entry) => set({ currentEntry: entry }),
}));
