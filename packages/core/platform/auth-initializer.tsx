"use client";

import { useEffect, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, getApi } from "../api";
import { useAuthStore } from "../auth";
import {
  captureSignupSource,
  identify as identifyAnalytics,
  initAnalytics,
  resetAnalytics,
} from "../analytics";
import { configStore } from "../config";
import { workspaceKeys } from "../workspace/queries";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";
import { setCurrentWorkspace } from "./workspace-storage";
import type { ClientIdentity } from "./types";
import type { StorageAdapter } from "../types/storage";
import type { User } from "../types";

const logger = createLogger("auth");

// 401 from the cold-start session probe just means "no active session" —
// expected for any anonymous page load. Logging it as an error pollutes the
// dev console and trains developers to ignore real auth failures, so route
// 401s through info and keep error reserved for unexpected breakage
// (network, 5xx, malformed response).
function logAuthInitFailure(message: string, err: unknown) {
  if (err instanceof ApiError && err.status === 401) {
    logger.info(message, { reason: "no active session" });
    return;
  }
  logger.error(message, err);
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

  useEffect(() => {
    const api = getApi();

    // Stamp attribution before anything else — the signup event (server-side)
    // reads this cookie, so it has to be present before the user hits submit.
    captureSignupSource();

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

    if (cookieAuth) {
      // Cookie mode: the HttpOnly cookie is sent automatically by the browser.
      // Call the API to check if the session is still valid.
      //
      // Seed the workspace list into React Query so the URL-driven layout can
      // resolve the slug without a second fetch. The active workspace itself
      // is derived from the URL by [workspaceSlug]/layout.tsx — no imperative
      // selection here.
      Promise.all([api.getMe(), api.listWorkspaces()])
        .then(([user, wsList]) => {
          onAuthSuccess(user);
          qc.setQueryData(workspaceKeys.list(), wsList);
        })
        .catch((err) => {
          logAuthInitFailure("cookie auth init failed", err);
          onAuthFailure();
        });
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

    Promise.all([api.getMe(), api.listWorkspaces()])
      .then(([user, wsList]) => {
        onAuthSuccess(user);
        // Seed React Query cache so the URL-driven layout can resolve the
        // slug without a second fetch.
        qc.setQueryData(workspaceKeys.list(), wsList);
      })
      .catch((err) => {
        logAuthInitFailure("auth init failed", err);
        api.setToken(null);
        setCurrentWorkspace(null, null);
        storage.removeItem("multica_token");
        onAuthFailure();
      });
  }, []);

  return <>{children}</>;
}
