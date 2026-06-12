"use client";

// Live member presence for the Slack-block rail. On mount it fetches the
// initial online snapshot, subscribes to `cerebro:presence` WS events to keep
// the set fresh, and runs a 30s heartbeat that POSTs presence/ping while the
// document is visible so this client stays "online" itself.
//
// Shared web+desktop hook: every `document`/`window` access is SSR-guarded.
import { useEffect, useRef, useState } from "react";
import type { WSEventType } from "@multica/core/types";
import { useWSEvent } from "@multica/core/realtime";
import { fetchPresenceSnapshot, pingPresence } from "../api/presence-api";

/** Shared contract: WS payload for the `cerebro:presence` event. */
interface PresencePayload {
  user_id: string;
  online: boolean;
}

// The presence/typing WS event strings are part of the shared contract but are
// added to the upstream `WSEventType` union by a separate worker. We cast the
// literal so this hook stays decoupled from that union's exact membership.
const PRESENCE_EVENT = "cerebro:presence" as WSEventType;

const PING_INTERVAL_MS = 30_000;

export interface UseMemberPresenceResult {
  onlineUserIds: Set<string>;
}

export function useMemberPresence(wsId: string): UseMemberPresenceResult {
  const [onlineUserIds, setOnlineUserIds] = useState<Set<string>>(
    () => new Set(),
  );

  // Mutates the online set immutably so React sees a new reference and
  // re-renders. No-op when the membership would not actually change.
  const applyPresence = (userId: string, online: boolean): void => {
    setOnlineUserIds((prev) => {
      if (online === prev.has(userId)) return prev;
      const next = new Set(prev);
      if (online) next.add(userId);
      else next.delete(userId);
      return next;
    });
  };

  // Keep the latest applyPresence in a ref so the WS handler identity is
  // stable (the handler passed to useWSEvent must not change every render).
  const applyRef = useRef(applyPresence);
  applyRef.current = applyPresence;

  // Subscribe to live presence events. The handler is stable across renders.
  const handlerRef = useRef((payload: unknown) => {
    const p = payload as PresencePayload | null;
    if (!p || typeof p.user_id !== "string") return;
    applyRef.current(p.user_id, p.online === true);
  });
  useWSEvent(PRESENCE_EVENT, handlerRef.current);

  // Initial snapshot + self-heartbeat. Re-runs when the workspace changes so
  // the snapshot and pings are scoped to the active workspace.
  useEffect(() => {
    let cancelled = false;

    const loadSnapshot = async (): Promise<void> => {
      const snapshot = await fetchPresenceSnapshot();
      if (cancelled) return;
      setOnlineUserIds(new Set(snapshot.online_user_ids));
    };

    const isVisible = (): boolean =>
      typeof document === "undefined" || document.visibilityState === "visible";

    // POST a heartbeat and fold the returned snapshot back in so a freshly
    // online peer appears even if its WS event was missed.
    const ping = async (): Promise<void> => {
      if (!isVisible()) return;
      const snapshot = await pingPresence();
      if (cancelled) return;
      setOnlineUserIds(new Set(snapshot.online_user_ids));
    };

    void loadSnapshot();
    void ping();

    const interval = setInterval(() => {
      void ping();
    }, PING_INTERVAL_MS);

    const onVisibility = (): void => {
      if (isVisible()) void ping();
    };
    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", onVisibility);
    }

    return () => {
      cancelled = true;
      clearInterval(interval);
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", onVisibility);
      }
    };
  }, [wsId]);

  return { onlineUserIds };
}
