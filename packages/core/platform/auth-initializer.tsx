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
import type { StorageAdapter } from "../types/storage";
import type { User } from "../types";

const logger = createLogger("auth");

export function AuthInitializer({
  children,
  onLogin,
  onLogout,
  storage = defaultStorage,
  cookieAuth,
}: {
  children: ReactNode;
  onLogin?: () => void;
  onLogout?: () => void;
  storage?: StorageAdapter;
  cookieAuth?: boolean;
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
        if (cfg.posthog_key) {
          initAnalytics({ key: cfg.posthog_key, host: cfg.posthog_host || "" });
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
      // Cookie mode first tries the trusted single-user bootstrap contract.
      // If the backend does not support it or bootstrap is otherwise
      // unavailable, fall back to the legacy "resume existing session via
      // getMe + listWorkspaces" path so compatibility flows keep working.
      api
        .bootstrap()
        .then(({ user, workspaces }) => {
          onAuthSuccess(user);
          qc.setQueryData(workspaceKeys.list(), workspaces);
        })
        .catch((bootstrapErr) => {
          logger.warn("bootstrap init failed; falling back to session check", bootstrapErr);
          Promise.all([api.getMe(), api.listWorkspaces()])
            .then(([user, wsList]) => {
              onAuthSuccess(user);
              qc.setQueryData(workspaceKeys.list(), wsList);
            })
            .catch((err) => {
              logger.error("cookie auth init failed", err);
              onAuthFailure();
            });
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
        logger.error("auth init failed", err);
        api.setToken(null);
        setCurrentWorkspace(null, null);
        storage.removeItem("multica_token");
        onAuthFailure();
      });
  }, []);

  return <>{children}</>;
}
