"use client";

import { useMemo } from "react";
import { ApiClient } from "../api/client";
import { setApiInstance } from "../api";
import { createAuthStore, registerAuthStore } from "../auth";
import { createWorkspaceStore, registerWorkspaceStore } from "../workspace";
import { createChatStore, registerChatStore } from "../chat";
import { WSProvider } from "../realtime";
import { QueryProvider } from "../provider";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";
import { AuthInitializer } from "./auth-initializer";
import type { CoreProviderProps } from "./types";
import type { StorageAdapter } from "../types/storage";

// Module-level singletons — created once at first render, never recreated.
// Vite HMR preserves module-level state, so these survive hot reloads.
let initialized = false;
let authStore: ReturnType<typeof createAuthStore>;
let workspaceStore: ReturnType<typeof createWorkspaceStore>;
let chatStore: ReturnType<typeof createChatStore>;
function initCore(
  apiBaseUrl: string,
  storage: StorageAdapter,
  onLogin?: () => void,
  onLogout?: () => void,
) {
  if (initialized) return;

  const api = new ApiClient(apiBaseUrl, {
    logger: createLogger("api"),
    onUnauthorized: () => {
      storage.removeItem("multica_token");
      storage.removeItem("multica_workspace_id");
    },
  });
  setApiInstance(api);

  // Hydrate token from storage
  const token = storage.getItem("multica_token");
  if (token) api.setToken(token);
  const wsId = storage.getItem("multica_workspace_id");
  if (wsId) api.setWorkspaceId(wsId);

  authStore = createAuthStore({ api, storage, onLogin, onLogout });
  registerAuthStore(authStore);

  workspaceStore = createWorkspaceStore(api, { storage });
  registerWorkspaceStore(workspaceStore);

  chatStore = createChatStore({ storage });
  registerChatStore(chatStore);

  initialized = true;
}

export function CoreProvider({
  children,
  apiBaseUrl = "",
  wsUrl = "ws://localhost:8080/ws",
  storage = defaultStorage,
  onLogin,
  onLogout,
}: CoreProviderProps) {
  // Initialize singletons on first render only. Dependencies are read-once:
  // apiBaseUrl, storage, and callbacks are set at app boot and never change at runtime.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useMemo(() => initCore(apiBaseUrl, storage, onLogin, onLogout), []);

  return (
    <QueryProvider>
      <AuthInitializer onLogin={onLogin} onLogout={onLogout} storage={storage}>
        <WSProvider
          wsUrl={wsUrl}
          authStore={authStore}
          workspaceStore={workspaceStore}
          storage={storage}
        >
          {children}
        </WSProvider>
      </AuthInitializer>
    </QueryProvider>
  );
}
