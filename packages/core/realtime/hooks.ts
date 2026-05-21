"use client";

import { useEffect } from "react";
import type { WSEventType } from "../types";
import { useWS } from "./provider";

type EventHandler = (payload: unknown, actorId?: string, actorType?: string) => void;

/**
 * Hook that subscribes to a WebSocket event and calls the handler.
 * Automatically unsubscribes on cleanup.
 */
export function useWSEvent(event: WSEventType, handler: EventHandler) {
  const { subscribe } = useWS();

  useEffect(() => {
    const unsub = subscribe(event, handler);
    return unsub;
  }, [event, handler, subscribe]);
}

/**
 * Hook that registers a callback to run on WebSocket reconnection.
 * Useful for refetching component-local data after a network interruption.
 */
export function useWSReconnect(callback: () => void) {
  const { onReconnect } = useWS();

  useEffect(() => {
    const unsub = onReconnect(callback);
    return unsub;
  }, [callback, onReconnect]);
}
