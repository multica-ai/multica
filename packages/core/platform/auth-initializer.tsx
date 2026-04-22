"use client";

import { useEffect, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getApi } from "../api";
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
import { ApiError } from "../api/client";

const logger = createLogger("auth");

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

    const seedWorkspaces = (wsList: unknown) => {
      qc.setQueryData(workspaceKeys.list(), wsList);
    };

    const applyBootstrap = (user: User, wsList: unknown, token?: string) => {
      if (token) {
        storage.setItem("multica_token", token);
        api.setToken(token);
      }
      onAuthSuccess(user);
      seedWorkspaces(wsList);
    };

    const runLegacyInit = () =>
      Promise.all([api.getMe(), api.listWorkspaces()]).then(([user, wsList]) => {
        onAuthSuccess(user);
        seedWorkspaces(wsList);
      });

    if (cookieAuth) {
      api
        .bootstrap()
        .then(({ user, workspaces }) => {
          applyBootstrap(user, workspaces);
        })
        .catch((err) => {
          if (err instanceof ApiError && err.status === 404) {
            runLegacyInit().catch((legacyErr) => {
              logger.error("cookie auth init failed", legacyErr);
              onAuthFailure();
            });
            return;
          }
          logger.error("cookie bootstrap init failed", err);
          onAuthFailure();
        });
      return;
    }

    const token = storage.getItem("multica_token");
    if (token) {
      api.setToken(token);
      runLegacyInit().catch((err) => {
        logger.error("auth init failed", err);
        api.setToken(null);
        setCurrentWorkspace(null, null);
        storage.removeItem("multica_token");
        api
          .bootstrapToken()
          .then(({ user, workspaces, token: bootstrapToken }) => {
            applyBootstrap(user, workspaces, bootstrapToken);
          })
          .catch((bootstrapErr) => {
            if (bootstrapErr instanceof ApiError && bootstrapErr.status === 404) {
              onLogout?.();
              useAuthStore.setState({ isLoading: false });
              return;
            }
            logger.error("bootstrap token init failed", bootstrapErr);
            onAuthFailure();
          });
      });
      return;
    }

    api
      .bootstrapToken()
      .then(({ user, workspaces, token: bootstrapToken }) => {
        applyBootstrap(user, workspaces, bootstrapToken);
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 404) {
          onLogout?.();
          useAuthStore.setState({ isLoading: false });
          return;
        }
        logger.error("bootstrap token init failed", err);
        onAuthFailure();
      });
  }, []);

  return <>{children}</>;
}
