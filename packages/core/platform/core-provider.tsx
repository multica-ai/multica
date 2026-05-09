"use client";

import { useMemo } from "react";
import { ApiClient } from "../api/client";
import { setApiInstance } from "../api";
import { createAuthStore, registerAuthStore } from "../auth";
import { createChatStore, registerChatStore } from "../chat";
import {
  I18nProvider,
  LocaleAdapterProvider,
  UserLocaleSync,
} from "../i18n/react";
import { WSProvider } from "../realtime";
import { QueryProvider } from "../provider";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";
import { AuthInitializer } from "./auth-initializer";
import { pickSessionToken } from "./session-token";
import type { CoreProviderProps, ClientIdentity } from "./types";
import type { StorageAdapter } from "../types/storage";

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

  // Hydrate the bearer token before any child query can fire. Order matters:
  // a session token (Feishu Project plugin iframe) wins over the legacy
  // localStorage token, and we install it synchronously here — the alternative
  // is letting AuthInitializer's effect do it, which races the first wave of
  // useQuery calls and leaves them stuck with cached 401s.
  const sessionToken = pickSessionToken();
  if (sessionToken) {
    api.setToken(sessionToken);
  } else if (!cookieAuth) {
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
  identity,
  locale,
  resources,
  localeAdapter,
}: CoreProviderProps) {
  // Initialize singletons on first render only. Dependencies are read-once:
  // apiBaseUrl, storage, and callbacks are set at app boot and never change at runtime.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useMemo(() => initCore(apiBaseUrl, storage, onLogin, onLogout, cookieAuth, identity), []);

  // I18nProvider wraps everything else: server and client must use the same
  // (locale, resources) to avoid hydration mismatch. Language switching goes
  // through window.location.reload(), never client-side changeLanguage.
  const tree = (
    <QueryProvider>
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

  // UserLocaleSync requires a LocaleAdapter to persist; only mount it when
  // the host app provides one (web layout + desktop App both do).
  const withAdapter = localeAdapter ? (
    <LocaleAdapterProvider adapter={localeAdapter}>
      <UserLocaleSync />
      {tree}
    </LocaleAdapterProvider>
  ) : (
    tree
  );

  return (
    <I18nProvider locale={locale} resources={resources}>
      {withAdapter}
    </I18nProvider>
  );
}
