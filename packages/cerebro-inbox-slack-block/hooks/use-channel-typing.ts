"use client";

// Live typing indicators for one channel/DM. Subscribes to `cerebro:typing`
// WS events for the given channelId; each event marks that user as typing for
// 5s (the per-user timer resets on every new event). `notifyTyping()` POSTs to
// the typing endpoint, throttled to at most once per 3s. The current user is
// excluded from the reported set.
//
// Shared web+desktop hook: no direct `document`/`window` access here, but the
// underlying REST call is SSR-guarded in presence-api.
import { useCallback, useEffect, useRef, useState } from "react";
import type { WSEventType } from "@multica/core/types";
import { useWSEvent } from "@multica/core/realtime";
import { useAuthStore } from "@multica/core/auth";
import { postTyping } from "../api/presence-api";

/** Shared contract: WS payload for the `cerebro:typing` event. */
interface TypingPayload {
  channel_id: string;
  user_id: string;
}

// Same decoupling rationale as use-member-presence: the literal is cast
// because the upstream WSEventType union is extended by a separate worker.
const TYPING_EVENT = "cerebro:typing" as WSEventType;

const TYPING_EXPIRY_MS = 5_000;
const NOTIFY_THROTTLE_MS = 3_000;

export interface UseChannelTypingResult {
  typingUserIds: Set<string>;
  notifyTyping: () => void;
}

export function useChannelTyping(
  channelId: string | null,
): UseChannelTypingResult {
  const currentUserId = useAuthStore((s) => s.user?.id);
  const [typingUserIds, setTypingUserIds] = useState<Set<string>>(
    () => new Set(),
  );

  // Per-user expiry timers so an idle typist auto-clears after 5s.
  const timersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(
    new Map(),
  );
  const lastNotifyRef = useRef(0);

  const clearTimers = useCallback(() => {
    for (const timer of timersRef.current.values()) clearTimeout(timer);
    timersRef.current.clear();
  }, []);

  // Stable WS handler. Reads channelId/currentUserId from refs so its identity
  // never changes — useWSEvent re-subscribes only when the event string does.
  const channelIdRef = useRef(channelId);
  channelIdRef.current = channelId;
  const currentUserIdRef = useRef(currentUserId);
  currentUserIdRef.current = currentUserId;

  const markTyping = useCallback((userId: string) => {
    setTypingUserIds((prev) => {
      if (prev.has(userId)) return prev;
      const next = new Set(prev);
      next.add(userId);
      return next;
    });
    const existing = timersRef.current.get(userId);
    if (existing) clearTimeout(existing);
    const timer = setTimeout(() => {
      timersRef.current.delete(userId);
      setTypingUserIds((prev) => {
        if (!prev.has(userId)) return prev;
        const next = new Set(prev);
        next.delete(userId);
        return next;
      });
    }, TYPING_EXPIRY_MS);
    timersRef.current.set(userId, timer);
  }, []);

  const handlerRef = useRef((payload: unknown) => {
    const p = payload as TypingPayload | null;
    if (!p || typeof p.user_id !== "string" || typeof p.channel_id !== "string")
      return;
    // Only events for the channel we're watching, and never our own typing.
    if (channelIdRef.current == null || p.channel_id !== channelIdRef.current)
      return;
    if (p.user_id === currentUserIdRef.current) return;
    markTyping(p.user_id);
  });
  useWSEvent(TYPING_EVENT, handlerRef.current);

  // Reset state + timers whenever the watched channel changes or unmounts.
  useEffect(() => {
    setTypingUserIds(new Set());
    return () => {
      clearTimers();
    };
  }, [channelId, clearTimers]);

  const notifyTyping = useCallback(() => {
    const id = channelIdRef.current;
    if (!id) return;
    const now = Date.now();
    if (now - lastNotifyRef.current < NOTIFY_THROTTLE_MS) return;
    lastNotifyRef.current = now;
    void postTyping(id);
  }, []);

  return { typingUserIds, notifyTyping };
}
