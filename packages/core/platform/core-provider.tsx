"use client";

import { useMemo, useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiClient } from "../api/client";
import { setApiInstance } from "../api";
import { createAuthStore, registerAuthStore } from "../auth";
import { createChatStore, registerChatStore } from "../chat";
import { WSProvider } from "../realtime";
import { QueryProvider } from "../provider";
import { createLogger } from "../logger";
import { setWorkspaceUrlHost } from "./workspace-url-host";
import { getCurrentWsId } from "./workspace-storage";
import { issueKeys } from "../issues/queries";
import { inboxKeys } from "../inbox/queries";
import { defaultStorage } from "./storage";
import { AuthInitializer } from "./auth-initializer";
import type { CoreProviderProps, ClientIdentity } from "./types";
import type { StorageAdapter } from "../types/storage";

function VisibilityRefetcher() {
  const queryClient = useQueryClient();
  useEffect(() => {
    function handleVisibilityChange() {
      if (document.visibilityState !== "visible") return;
      const wsId = getCurrentWsId();
      if (!wsId) return;
      queryClient.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      queryClient.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
    }
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, [queryClient]);
  return null;
}

// Module-level singletons — created once at first render, never recreated.
// Vite HMR preserves module-level state, so these survive hot reloads.
let initialized = false;
let authStore: ReturnType<typeof createAuthStore>;
let chatStore: ReturnType<typeof createChatStore>;
function initCore(
  apiBaseUrl: string,
  storage: StorageAdapter,
  onLogin?: () => void,
  onLogout?: () => void,
  cookieAuth?: boolean,
  identity?: ClientIdentity,
) {
  if (initialized) return;

  const api = new ApiClient(apiBaseUrl, {
    logger: createLogger("api"),
    onUnauthorized: () => {
      storage.removeItem("multica_token");
    },
    identity,
  });
  setApiInstance(api);

  // In token mode, hydrate token from storage.
  if (!cookieAuth) {
    const token = storage.getItem("multica_token");
    if (token) api.setToken(token);
  }
  // Workspace identity is URL-driven: the [workspaceSlug] layout resolves
  // the slug and calls setCurrentWorkspace(slug, wsId) on mount. The api
  // client reads the slug from that singleton for the X-Workspace-Slug
  // header. No boot-time hydration from storage is required.

  authStore = createAuthStore({ api, storage, onLogin, onLogout, cookieAuth });
  registerAuthStore(authStore);

  chatStore = createChatStore({ storage });
  registerChatStore(chatStore);

  initialized = true;
}

export function CoreProvider({
  children,
  apiBaseUrl = "",
  wsUrl = "ws://localhost:8080/ws",
  storage = defaultStorage,
  cookieAuth,
  onLogin,
  onLogout,
  workspaceUrlHost: workspaceUrlHostProp,
  identity,
}: CoreProviderProps) {
  // Initialize singletons on first render only. Dependencies are read-once:
  // apiBaseUrl, storage, and callbacks are set at app boot and never change at runtime.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useMemo(() => initCore(apiBaseUrl, storage, onLogin, onLogout, cookieAuth, identity), []);

  setWorkspaceUrlHost(workspaceUrlHostProp);

  return (
    <QueryProvider>
      <VisibilityRefetcher />
      <AuthInitializer
        onLogin={onLogin}
        onLogout={onLogout}
        storage={storage}
        cookieAuth={cookieAuth}
        identity={identity}
      >
        <WSProvider
          wsUrl={wsUrl}
          authStore={authStore}
          storage={storage}
          cookieAuth={cookieAuth}
          identity={identity}
        >
          {children}
        </WSProvider>
      </AuthInitializer>
    </QueryProvider>
  );
}
