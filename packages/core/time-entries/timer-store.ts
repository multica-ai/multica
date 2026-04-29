import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import { createPersistStorage } from "../platform/persist-storage";
import { defaultStorage } from "../platform/storage";

export interface ActiveTimer {
  issueId: string;
  issueIdentifier: string;
  issueTitle: string;
  startedAt: number; // epoch ms — elapsed computed from Date.now() - startedAt
  activityId?: number;
  activityName?: string;
}

export interface TimerState {
  activeTimer: ActiveTimer | null;

  startTimer: (issueId: string, identifier: string, title: string) => void;
  stopTimer: () => {
    issueId: string;
    durationMinutes: number;
    startedAt: string;
    stoppedAt: string;
  } | null;
  discardTimer: () => void;
  setActivity: (id: number, name: string) => void;
}

export const useTimerStore = create<TimerState>()(
  persist(
    (set, get) => ({
      activeTimer: null,

      startTimer: (issueId, identifier, title) => {
        set({
          activeTimer: {
            issueId,
            issueIdentifier: identifier,
            issueTitle: title,
            startedAt: Date.now(),
          },
        });
      },

      stopTimer: () => {
        const timer = get().activeTimer;
        if (!timer) return null;

        const now = Date.now();
        const elapsedMs = now - timer.startedAt;
        const durationMinutes = Math.max(1, Math.round(elapsedMs / 60000));

        set({ activeTimer: null });

        return {
          issueId: timer.issueId,
          durationMinutes,
          startedAt: new Date(timer.startedAt).toISOString(),
          stoppedAt: new Date(now).toISOString(),
        };
      },

      discardTimer: () => {
        set({ activeTimer: null });
      },

      setActivity: (id, name) => {
        const timer = get().activeTimer;
        if (!timer) return;
        set({
          activeTimer: { ...timer, activityId: id, activityName: name },
        });
      },
    }),
    {
      name: "multica_timer",
      storage: createJSONStorage(() => createPersistStorage(defaultStorage)),
      partialize: (state) => ({ activeTimer: state.activeTimer }),
    },
  ),
);
