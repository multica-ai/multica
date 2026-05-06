"use client";

import { useEffect, type ReactNode } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { getApi } from "../api";
import type { ApiClient } from "../api/client";
import { useAuthStore } from "../auth";
import {
  captureSignupSource,
  identify as identifyAnalytics,
  initAnalytics,
  resetAnalytics,
} from "../analytics";
import { configStore } from "../config";
import { isLocalOnlyProduct, type ProductCapabilities } from "../config/local-product";
import { workspaceKeys } from "../workspace/queries";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";
import { useProductCapabilities } from "./product-capabilities";
import { setCurrentWorkspace } from "./workspace-storage";
import type { ClientIdentity } from "./types";
import type { StorageAdapter } from "../types/storage";
import type { User } from "../types";

const logger = createLogger("auth");

interface AuthBootstrapOptions {
  api: ApiClient;
  qc: QueryClient;
  storage: StorageAdapter;
  capabilities: ProductCapabilities;
  cookieAuth?: boolean;
  identity?: ClientIdentity;
  onLogin?: () => void;
  onLogout?: () => void;
}

/**
 * Pure bootstrap routine. The component-side `AuthInitializer` is just a
 * thin `useEffect` wrapper around this function — extracted so the three
 * branches (local-only, cookie, token) can be unit-tested without rendering
 * React.
 */
export async function runAuthBootstrap({
  api,
  qc,
  storage,
  capabilities,
  cookieAuth,
  identity,
  onLogin,
  onLogout,
}: AuthBootstrapOptions): Promise<void> {
  const onAuthSuccess = (user: User) => {
    onLogin?.();
    useAuthStore.setState({ user, isLoading: false });
    identifyAnalytics(user.id, { email: user.email, name: user.name });
  };

  const onAuthFailure = () => {
    onLogout?.();
    resetAnalytics();
    useAuthStore.setState({ user: null, isLoading: false });
  };

  const localOnly = isLocalOnlyProduct(capabilities);

  // Stamp marketing attribution before anything else — only meaningful when
  // a real signup can happen. In a local install there's no signup, so this
  // would just plant a useless cookie on first launch.
  if (!localOnly) {
    captureSignupSource();
  }

  // Fetch app config (CDN domain, PostHog key, …) in the background — non-blocking.
  api
    .getConfig()
    .then((cfg) => {
      if (cfg.cdn_domain) configStore.getState().setCdnDomain(cfg.cdn_domain);
      configStore.getState().setAuthConfig({
        allowSignup: cfg.allow_signup,
        googleClientId: cfg.google_client_id,
      });
      if (cfg.posthog_key) {
        initAnalytics({
          key: cfg.posthog_key,
          host: cfg.posthog_host || "",
          appVersion: identity?.version,
        });
      }
    })
    .catch(() => {
      /* config is optional — legacy file card matching degrades gracefully */
    });

  if (localOnly) {
    // Local mode: ask the server to mint a session for the local user/space.
    // The server is the source of truth for which identity gets the token;
    // the client never picks one. A 404 here means the server is NOT running
    // in local mode — surface it as a hard logged-out state.
    try {
      const { token, user } = await api.localSession();
      storage.setItem("multica_token", token);
      api.setToken(token);
      const [, wsList] = await Promise.all([
        Promise.resolve(user),
        api.listWorkspaces(),
      ]);
      onAuthSuccess(user);
      qc.setQueryData(workspaceKeys.list(), wsList);
    } catch (err) {
      logger.error("local session bootstrap failed", err);
      onAuthFailure();
    }
    return;
  }

  if (cookieAuth) {
    // Cookie mode: the HttpOnly cookie is sent automatically by the browser.
    // Call the API to check if the session is still valid.
    //
    // Seed the workspace list into React Query so the URL-driven layout can
    // resolve the slug without a second fetch. The active workspace itself
    // is derived from the URL by [workspaceSlug]/layout.tsx — no imperative
    // selection here.
    try {
      const [user, wsList] = await Promise.all([
        api.getMe(),
        api.listWorkspaces(),
      ]);
      onAuthSuccess(user);
      qc.setQueryData(workspaceKeys.list(), wsList);
    } catch (err) {
      logger.error("cookie auth init failed", err);
      onAuthFailure();
    }
    return;
  }

  // Token mode: read from localStorage (Electron / legacy).
  const token = storage.getItem("multica_token");
  if (!token) {
    onLogout?.();
    useAuthStore.setState({ isLoading: false });
    return;
  }

  api.setToken(token);

  try {
    const [user, wsList] = await Promise.all([
      api.getMe(),
      api.listWorkspaces(),
    ]);
    onAuthSuccess(user);
    // Seed React Query cache so the URL-driven layout can resolve the
    // slug without a second fetch.
    qc.setQueryData(workspaceKeys.list(), wsList);
  } catch (err) {
    logger.error("auth init failed", err);
    api.setToken(null);
    setCurrentWorkspace(null, null);
    storage.removeItem("multica_token");
    onAuthFailure();
  }
}

export function AuthInitializer({
  children,
  onLogin,
  onLogout,
  storage = defaultStorage,
  cookieAuth,
  identity,
}: {
  children: ReactNode;
  onLogin?: () => void;
  onLogout?: () => void;
  storage?: StorageAdapter;
  cookieAuth?: boolean;
  identity?: ClientIdentity;
}) {
  const qc = useQueryClient();
  const capabilities = useProductCapabilities();

  useEffect(() => {
    void runAuthBootstrap({
      api: getApi(),
      qc,
      storage,
      capabilities,
      cookieAuth,
      identity,
      onLogin,
      onLogout,
    });
    // Boot-time effect — dependencies are read-once at mount. Reading them
    // reactively would re-run the entire bootstrap on every render of any
    // ancestor, which is never what we want.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return <>{children}</>;
}
