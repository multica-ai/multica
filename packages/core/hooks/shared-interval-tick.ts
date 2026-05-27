import { useSyncExternalStore } from "react";

export function createSharedIntervalTick(intervalMs: number): () => number {
  let tick = 0;
  let timer: ReturnType<typeof setInterval> | null = null;
  const listeners = new Set<() => void>();

  const getSnapshot = () => tick;

  const subscribe = (listener: () => void) => {
    listeners.add(listener);
    if (!timer) {
      timer = setInterval(() => {
        tick += 1;
        for (const l of listeners) l();
      }, intervalMs);
    }

    return () => {
      listeners.delete(listener);
      if (listeners.size === 0 && timer) {
        clearInterval(timer);
        timer = null;
      }
    };
  };

  return function useSharedIntervalTick(): number {
    return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  };
}
