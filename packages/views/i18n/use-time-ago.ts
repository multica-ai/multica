import { useCallback, useSyncExternalStore } from "react";
import { useT } from "./use-t";

// Relative labels ("just now", "4m ago") are derived from Date.now(), so they
// go stale the moment a parent stops re-rendering. Comment cards, board cards
// and activity rows are memoized, and nothing else on those surfaces changes
// while the user just sits on the page — so "4m ago" used to stay "4m ago"
// forever (multica-ai/multica#7899).
//
// One module-level ticker fixes every call site at once. useTimeAgo() subscribes
// through useSyncExternalStore, so a tick re-renders only the components that
// actually format a time; callers keep `const timeAgo = useTimeAgo()` and do
// not thread a `now` prop through memo boundaries. The interval is shared: the
// first subscriber starts it, the last unsubscribe clears it, so a timeline
// with dozens of timestamps still owns exactly one timer.
//
// 30s is finer than the coarsest label we show (whole minutes) and coarse
// enough that a long-lived page does not re-render its comment list every
// second. The client snapshot is a number kept in the module — never a fresh
// Date.now() on every call, which useSyncExternalStore would treat as an
// endless change. The clock is re-read whenever a subscriber mounts, so a
// module imported long before the first render (a server that has been up for
// hours, a test file whose fixtures are built at import time) does not pin a
// stale time. Server rendering reads the clock for that request only.
const TICK_MS = 30_000;

let now = Date.now();
let timer: ReturnType<typeof setInterval> | null = null;
const listeners = new Set<() => void>();

function subscribe(onStoreChange: () => void): () => void {
  listeners.add(onStoreChange);
  if (timer === null) {
    timer = setInterval(() => {
      now = Date.now();
      for (const listener of listeners) listener();
    }, TICK_MS);
  }
  // Re-read the clock when a component actually mounts. The module may have been
  // imported long before the first render (a long-lived SSR process, a test file
  // whose fixtures are built at import time), and useSyncExternalStore re-checks
  // the snapshot right after subscribing, so this replaces a stale first render.
  now = Date.now();
  return () => {
    listeners.delete(onStoreChange);
    if (listeners.size === 0 && timer !== null) {
      clearInterval(timer);
      timer = null;
    }
  };
}

function getSnapshot(): number {
  return now;
}

// A server process can live for hours between the module import and a request,
// so the server always renders the clock as of that render.
function getServerSnapshot(): number {
  return Date.now();
}

// Localized relative-time formatter. Returns a function so call-site usage
// stays terse: `const timeAgo = useTimeAgo(); ...timeAgo(dateStr)`.
export function useTimeAgo() {
  const { t } = useT("common");
  const current = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
  return useCallback(
    (dateStr: string): string => {
      const diff = current - new Date(dateStr).getTime();
      const minutes = Math.floor(diff / 60000);
      if (minutes < 1) return t(($) => $.time.just_now);
      if (minutes < 60) return t(($) => $.time.minutes_ago, { count: minutes });
      const hours = Math.floor(minutes / 60);
      if (hours < 24) return t(($) => $.time.hours_ago, { count: hours });
      const days = Math.floor(hours / 24);
      return t(($) => $.time.days_ago, { count: days });
    },
    [current, t],
  );
}
