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
import type { Logger } from "../logger";

// Module-level singletons — created once at first render, never recreated.
// Vite HMR preserves module-level state, so these survive hot reloads.
let initialized = false;
let authStore: ReturnType<typeof createAuthStore>;
let workspaceStore: ReturnType<typeof createWorkspaceStore>;
let chatStore: ReturnType<typeof createChatStore>;
function initCore(
  apiBaseUrl: string,
  storage: StorageAdapter,
  apiLogger?: Logger,
  onLogin?: () => void,
  onLogout?: () => void,
  cookieAuth?: boolean,
) {
  if (initialized) return;

  const api = new ApiClient(apiBaseUrl, {
    logger: apiLogger ?? createLogger("api"),
    onUnauthorized: () => {
      storage.removeItem("multica_token");
      storage.removeItem("multica_workspace_id");
    },
  });
  setApiInstance(api);

  // In token mode, hydrate token from storage.
  if (!cookieAuth) {
    const token = storage.getItem("multica_token");
    if (token) api.setToken(token);
  }
  const wsId = storage.getItem("multica_workspace_id");
  if (wsId) api.setWorkspaceId(wsId);

  authStore = createAuthStore({ api, storage, onLogin, onLogout, cookieAuth });
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
  apiLogger,
  wsUrl = "ws://localhost:8080/ws",
  storage = defaultStorage,
  cookieAuth,
  queryTokenAuth,
  wsHeaders,
  onLogin,
  onLogout,
}: CoreProviderProps) {
  // Initialize singletons on first render only. Dependencies are read-once:
  // apiBaseUrl, storage, logger, and callbacks are set at app boot and never change at runtime.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useMemo(() => initCore(apiBaseUrl, storage, apiLogger, onLogin, onLogout, cookieAuth), []);

  return (
    <QueryProvider>
      <AuthInitializer onLogin={onLogin} onLogout={onLogout} storage={storage} cookieAuth={cookieAuth}>
        <WSProvider
          wsUrl={wsUrl}
          authStore={authStore}
          workspaceStore={workspaceStore}
          storage={storage}
          cookieAuth={cookieAuth}
          queryTokenAuth={queryTokenAuth}
          wsHeaders={wsHeaders}
        >
          {children}
        </WSProvider>
      </AuthInitializer>
    </QueryProvider>
  );
}
