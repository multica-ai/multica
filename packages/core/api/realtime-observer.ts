import type { WSMessage } from "../types/events";

/**
 * Development hook into realtime sync: which WebSocket frame caused which
 * query cache change, plus a way to run a frame through the sync handlers
 * again. Nothing registers an observer in production; the dev toolbar's
 * recorder is the only one. Without it the helpers below pass straight
 * through and no frame is ever tagged.
 */
export interface RealtimeObserver {
  /** A frame arrived, or is being replayed. Returns the id its cache changes are credited to. */
  onEvent(msg: WSMessage, replayed: boolean): number;
  /** The socket came back after a drop. Returns the id the catch-up refetch is credited to. */
  onReconnect(): number;
}

/** What realtime sync offers for local debugging. */
export interface RealtimeSyncTarget {
  /** Run a frame through realtime sync's own handlers, not every subscriber. */
  replay(msg: WSMessage): void;
  /** Close the socket the way a network drop would, so the client reconnects. */
  drop(): void;
}

/** Ids of the frames (or reconnects) whose handling is running right now. */
export type SyncCauses = readonly number[];

const NONE: SyncCauses = [];
// A debounced refresh coalesces every frame that asked for it; a burst of
// hundreds should not grow the list without bound.
const MAX_HELD_CAUSES = 20;

let observer: RealtimeObserver | null = null;
let target: RealtimeSyncTarget | null = null;
let active: SyncCauses = NONE;

export function setRealtimeObserver(next: RealtimeObserver | null): void {
  observer = next;
}

export function getRealtimeObserver(): RealtimeObserver | null {
  return observer;
}

export function activeSyncCauses(): SyncCauses {
  return active;
}

/** Run `fn` with the cache changes it makes credited to `causes`. */
export function runWithSyncCauses<T>(causes: SyncCauses, fn: () => T): T {
  if (causes.length === 0) return fn();
  const previous = active;
  active = causes;
  try {
    return fn();
  } finally {
    active = previous;
  }
}

/**
 * The causes running now, added to `held`. Work deferred to a timer holds
 * these and later runs under them, so a debounced refetch is still credited
 * to the frames that asked for it. Work after an `await` is not followed.
 */
export function holdSyncCauses(held: SyncCauses = NONE): SyncCauses {
  if (active.length === 0) return held;
  if (held.length === 0) return active;
  return [...new Set([...held, ...active])].slice(-MAX_HELD_CAUSES);
}

/** Realtime sync registers itself while subscribed; returns the unregister. */
export function registerRealtimeSyncTarget(next: RealtimeSyncTarget): () => void {
  target = next;
  return () => {
    if (target === next) target = null;
  };
}

/** Run a recorded frame through realtime sync again. False when nothing is subscribed. */
export function replayRealtimeEvent(msg: WSMessage): boolean {
  const current = target;
  if (!current) return false;
  const id = observer?.onEvent(msg, true);
  runWithSyncCauses(id === undefined ? NONE : [id], () => current.replay(msg));
  return true;
}

/** Drop the live socket so it reconnects. False when there is none. */
export function dropRealtimeConnection(): boolean {
  if (!target) return false;
  target.drop();
  return true;
}
