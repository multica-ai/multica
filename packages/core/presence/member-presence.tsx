"use client";

// CEREBRO-PATCH(member-presence-core): workspace-wide member (human) presence.
// TECH-3583 — the red/green online/offline status dot used to exist only for
// AGENTS (via availabilityConfig). Humans had no shared presence source that
// the common ActorAvatar could read, so people rendered with NO dot in every
// picker / @-mention / assignee surface. This module lifts the slack-block's
// presence backbone into @multica/core and exposes it through a provider +
// context so any avatar can show a human's online state.
//
// One heartbeat per tab: the provider runs the snapshot+ping loop once for the
// whole workspace; every consumer (ActorAvatar, the slack-block rail, …) reads
// the shared set from context instead of opening its own subscription.
//
// Shared web+desktop module: every `document`/`window` access is SSR-guarded.
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { WSEventType } from "../types";
import { useWSEvent } from "../realtime";
import { fetchPresenceSnapshot, pingPresence } from "./presence-api";

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

// Context value: the live online set, or `null` when no provider is mounted
// above (e.g. in isolated tests or surfaces outside the workspace shell). A
// `null` context means "presence unknown" — consumers render no dot rather
// than guessing offline.
const MemberPresenceContext = createContext<Set<string> | null>(null);

// Raw heartbeat: fetches the initial snapshot, subscribes to `cerebro:presence`
// WS events, and runs a 30s visible-tab ping so this client stays "online".
// `enabled` lets a consumer that already has presence from context skip the
// subscription entirely (hook order stays stable — the effect just no-ops).
function useMemberPresenceHeartbeat(
  wsId: string,
  enabled: boolean,
): Set<string> {
  const [onlineUserIds, setOnlineUserIds] = useState<Set<string>>(
    () => new Set(),
  );

  const applyPresence = (userId: string, online: boolean): void => {
    setOnlineUserIds((prev) => {
      if (online === prev.has(userId)) return prev;
      const next = new Set(prev);
      if (online) next.add(userId);
      else next.delete(userId);
      return next;
    });
  };

  const applyRef = useRef(applyPresence);
  applyRef.current = applyPresence;

  const handlerRef = useRef((payload: unknown) => {
    if (!enabledRef.current) return;
    const p = payload as PresencePayload | null;
    if (!p || typeof p.user_id !== "string") return;
    applyRef.current(p.user_id, p.online === true);
  });
  // Keep an enabled flag the WS handler can read without re-subscribing.
  const enabledRef = useRef(enabled);
  enabledRef.current = enabled;
  useWSEvent(PRESENCE_EVENT, handlerRef.current);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;

    const loadSnapshot = async (): Promise<void> => {
      const snapshot = await fetchPresenceSnapshot();
      if (cancelled) return;
      setOnlineUserIds(new Set(snapshot.online_user_ids));
    };

    const isVisible = (): boolean =>
      typeof document === "undefined" || document.visibilityState === "visible";

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
  }, [wsId, enabled]);

  return onlineUserIds;
}

/**
 * Mount once per workspace (in the app shell). Runs the single presence
 * heartbeat for the tab and shares the online set with every descendant
 * avatar / list via context.
 */
export function MemberPresenceProvider({
  wsId,
  children,
}: {
  wsId: string;
  children: ReactNode;
}) {
  const onlineUserIds = useMemberPresenceHeartbeat(wsId, true);
  return (
    <MemberPresenceContext.Provider value={onlineUserIds}>
      {children}
    </MemberPresenceContext.Provider>
  );
}

/**
 * Back-compat hook for surfaces that want the full online set (e.g. the
 * slack-block rail). Context-aware: when a provider is mounted above (the
 * normal case inside the workspace shell) it returns the shared set and opens
 * NO second subscription; otherwise it self-manages a heartbeat so isolated
 * usage keeps working.
 */
export function useMemberPresence(wsId: string): UseMemberPresenceResult {
  const ctx = useContext(MemberPresenceContext);
  const self = useMemberPresenceHeartbeat(wsId, ctx === null);
  return { onlineUserIds: ctx ?? self };
}

/**
 * Per-user online state for a single member.
 * Returns `true`/`false` when presence is known, or `null` when no provider is
 * mounted above — callers render no dot on `null` rather than assuming offline.
 */
export function useMemberOnline(userId: string | undefined): boolean | null {
  const ctx = useContext(MemberPresenceContext);
  if (ctx === null || !userId) return null;
  return ctx.has(userId);
}
