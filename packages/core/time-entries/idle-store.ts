import { create } from "zustand";

const IDLE_THRESHOLD_MS = 15 * 60 * 1000; // 15 minutes
const CHECK_INTERVAL_MS = 30 * 1000; // check every 30 seconds

export interface IdleState {
  lastActivityAt: number;
  isIdle: boolean;
  idleSince: number | null;

  recordActivity: () => void;
  checkIdle: () => void;
  dismissIdle: () => void;
}

export const useIdleStore = create<IdleState>()((set, get) => ({
  lastActivityAt: Date.now(),
  isIdle: false,
  idleSince: null,

  recordActivity: () => {
    // Only reset the idle timer. Do NOT clear isIdle here — the dialog must
    // remain visible until the user explicitly acts on it via dismissIdle().
    set({ lastActivityAt: Date.now() });
  },

  checkIdle: () => {
    const { lastActivityAt, isIdle } = get();
    const elapsed = Date.now() - lastActivityAt;
    if (elapsed >= IDLE_THRESHOLD_MS && !isIdle) {
      set({ isIdle: true, idleSince: lastActivityAt });
    }
  },

  dismissIdle: () => {
    set({ isIdle: false, idleSince: null, lastActivityAt: Date.now() });
  },
}));

// Auto-start activity tracking when this module loads
let _intervalId: ReturnType<typeof setInterval> | null = null;
let _listenersAttached = false;

export function startIdleTracking() {
  if (_listenersAttached) return;
  _listenersAttached = true;

  const record = () => useIdleStore.getState().recordActivity();

  // Track user activity events
  if (typeof window !== "undefined") {
    const events: (keyof WindowEventMap)[] = [
      "mousemove",
      "mousedown",
      "keydown",
      "scroll",
      "touchstart",
    ];
    for (const evt of events) {
      window.addEventListener(evt, record, { passive: true });
    }
  }

  // Periodic idle check
  _intervalId = setInterval(() => {
    useIdleStore.getState().checkIdle();
  }, CHECK_INTERVAL_MS);
}

export function stopIdleTracking() {
  if (_intervalId) {
    clearInterval(_intervalId);
    _intervalId = null;
  }
  _listenersAttached = false;
}
