"use client";

import {
  createContext,
  useContext,
  useEffect,
  useState,
  useCallback,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { WSClient } from "../api/ws-client";
import type { WSEventType, StorageAdapter } from "../types";
import type { StoreApi, UseBoundStore } from "zustand";
import type { AuthState } from "../auth/store";
import {
  getCurrentSlug,
  subscribeToCurrentSlug,
} from "../platform/workspace-storage";
import { createLogger } from "../logger";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

type EventHandler = (payload: unknown, actorId?: string) => void;

interface WSContextValue {
  subscribe: (event: WSEventType, handler: EventHandler) => () => void;
  onReconnect: (callback: () => void) => () => void;
}

const WSContext = createContext<WSContextValue | null>(null);

export interface WSProviderProps {
  children: ReactNode;
  /** WebSocket server URL (e.g. "ws://localhost:8080/ws") */
  wsUrl: string;
  /** Platform-created auth store instance */
  authStore: UseBoundStore<StoreApi<AuthState>>;
  /** Platform-specific storage adapter for reading auth tokens */
  storage: StorageAdapter;
  /** When true, use HttpOnly cookies instead of token query param for WS auth. */
  cookieAuth?: boolean;
  /** Optional callback for showing toast messages (platform-specific, e.g. sonner) */
  onToast?: (message: string, type?: "info" | "error") => void;
}

export function WSProvider({
  children,
  wsUrl,
  authStore,
  storage,
  cookieAuth,
  onToast,
}: WSProviderProps) {
  const user = authStore((s) => s.user);
  // Reactive read of the current workspace slug (URL-driven singleton in
  // packages/core/platform/workspace-storage.ts). When the workspace switches,
  // the useEffect below tears down the old WS connection and opens a new one
  // bound to the new workspace slug. SSR snapshot is `null` because this
  // provider only renders client-side under CoreProvider.
  const wsSlug = useSyncExternalStore(
    subscribeToCurrentSlug,
    getCurrentSlug,
    () => null,
  );
  const [wsClient, setWsClient] = useState<WSClient | null>(null);

  useEffect(() => {
    if (!user || !wsSlug) return;

    // In token mode we need a token from storage; in cookie mode the HttpOnly
    // cookie is sent automatically with the WS upgrade request.
    const token = cookieAuth ? null : storage.getItem("multica_token");
    if (!cookieAuth && !token) return;

    const ws = new WSClient(wsUrl, {
      logger: createLogger("ws"),
      cookieAuth,
    });
    ws.setAuth(token, wsSlug);
    setWsClient(ws);
    ws.connect();

    return () => {
      ws.disconnect();
      setWsClient(null);
    };
  }, [user, wsSlug, wsUrl, storage, cookieAuth]);

  const stores: RealtimeSyncStores = { authStore };

  // Centralized WS -> store sync (uses state so it re-subscribes when WS changes)
  useRealtimeSync(wsClient, stores, onToast);

  const subscribe = useCallback(
    (event: WSEventType, handler: EventHandler) => {
      if (!wsClient) return () => {};
      return wsClient.on(event, handler);
    },
    [wsClient],
  );

  const onReconnectCb = useCallback(
    (callback: () => void) => {
      if (!wsClient) return () => {};
      return wsClient.onReconnect(callback);
    },
    [wsClient],
  );

  return (
    <WSContext.Provider value={{ subscribe, onReconnect: onReconnectCb }}>
      {children}
    </WSContext.Provider>
  );
}

export function useWS() {
  const ctx = useContext(WSContext);
  if (!ctx) throw new Error("useWS must be used within WSProvider");
  return ctx;
}
