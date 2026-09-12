import { useSyncExternalStore } from "react";

const listeners = new Set<() => void>();
let tick = 0;
let timer: ReturnType<typeof setInterval> | undefined;

function notify() {
  tick += 1;
  listeners.forEach((listener) => listener());
}

function stopTimer() {
  clearInterval(timer);
  timer = undefined;
}

function resume() {
  if (document.visibilityState === "hidden") {
    stopTimer();
    return;
  }
  notify();
  timer ??= setInterval(notify, 30_000);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  if (listeners.size === 1) {
    document.addEventListener("visibilitychange", resume);
    window.addEventListener("focus", resume);
    resume();
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      stopTimer();
      document.removeEventListener("visibilitychange", resume);
      window.removeEventListener("focus", resume);
    }
  };
}

// One clock per renderer, subscribed only by relative-time text leaves. Hidden
// documents and unmounted timelines do not keep an interval running.
export function useRelativeTimeTick(): number {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

function getSnapshot() {
  return tick;
}

function getServerSnapshot() {
  return 0;
}
