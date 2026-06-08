"use client";

import type { StoreApi, UseBoundStore } from "zustand";
import type { WSClient } from "../api/ws-client";
import type { AuthState } from "../auth/store";
import { useRealtimeSync } from "./use-realtime-sync";

interface RealtimeSyncRuntimeProps {
  wsClient: WSClient;
  authStore: UseBoundStore<StoreApi<AuthState>>;
  onToast?: (message: string, type?: "info" | "error") => void;
}

export function RealtimeSyncRuntime({
  wsClient,
  authStore,
  onToast,
}: RealtimeSyncRuntimeProps) {
  useRealtimeSync(wsClient, { authStore }, onToast);
  return null;
}
