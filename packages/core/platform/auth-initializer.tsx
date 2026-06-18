"use client";

import { useEffect, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getApi } from "../api";
import {
  isTransientAuthProbeError,
  isUnauthorizedError,
} from "../api/client";
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

function authRetryDelayMs(attempt: number): number {
  return Math.min(30_000, 1_000 * 2 ** Math.min(attempt, 5));
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
    let cancelled = false;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let authAttempt = 0;

    const clearRetry = () => {
      if (retryTimer) {
        clearTimeout(retryTimer);
        retryTimer = null;
      }
    };

    // Stamp attribution before anything else — the signup event (server-side)
    // reads this cookie, so it has to be present before the user hits submit.
    captureSignupSource();

    // Fetch app config (CDN domain, PostHog key, …) in the background — non-blocking.
    api
      .getConfig()
      .then((cfg) => {
        if (cfg.cdn_domain) {
          configStore.getState().setCdnConfig({
            cdnDomain: cfg.cdn_domain,
            // Old servers omit this — false keeps the previous behavior.
            cdnSigned: cfg.cdn_signed === true,
          });
        }
        configStore.getState().setAuthConfig({
          allowSignup: cfg.allow_signup,
          googleClientId: cfg.google_client_id,
          // Old servers omit this field — treat that as "creation allowed"
          // (the managed-cloud default) rather than blocking the UI.
          workspaceCreationDisabled: cfg.workspace_creation_disabled === true,
        });
        configStore.getState().setDaemonConfig({
          daemonServerUrl: cfg.daemon_server_url,
          daemonAppUrl: cfg.daemon_app_url,
        });
        if (cfg.posthog_key) {
          initAnalytics({
            key: cfg.posthog_key,
            host: cfg.posthog_host || "",
            appVersion: identity?.version,
            environment: cfg.analytics_environment,
          });
        }
      })
      .catch(() => {
        /* config is optional — legacy file card matching degrades gracefully */
      });

    const onAuthSuccess = (user: User) => {
      if (cancelled) return;
      authAttempt = 0;
      onLogin?.();
      useAuthStore.setState({
        user,
        isLoading: false,
        authStatus: "authenticated",
        authUnavailableSince: null,
      });
      identifyAnalytics(user.id, { email: user.email, name: user.name });
    };

    const onAuthFailure = () => {
      if (cancelled) return;
      onLogout?.();
      resetAnalytics();
      api.setToken(null);
      setCurrentWorkspace(null, null);
      useAuthStore.setState({
        user: null,
        isLoading: false,
        authStatus: "unauthenticated",
        authUnavailableSince: null,
      });
    };

    const onAuthTemporarilyUnavailable = (err: unknown) => {
      if (cancelled) return;
      logger.warn("auth init temporarily unavailable", err);
      useAuthStore.setState((state) => ({
        user: state.user,
        isLoading: false,
        authStatus: "temporarily_unreachable",
        authUnavailableSince: state.authUnavailableSince ?? Date.now(),
      }));
      const delay = authRetryDelayMs(authAttempt);
      authAttempt += 1;
      clearRetry();
      retryTimer = setTimeout(runAuthProbe, delay);
    };

    const seedWorkspaceList = () => {
      api
        .listWorkspaces()
        .then((wsList) => {
          if (!cancelled) qc.setQueryData(workspaceKeys.list(), wsList);
        })
        .catch((err) => {
          logger.warn("workspace list seed failed during auth init", err);
        });
    };

    const handleAuthProbeError = (err: unknown) => {
      if (isUnauthorizedError(err)) {
        onAuthFailure();
        return;
      }
      if (isTransientAuthProbeError(err)) {
        onAuthTemporarilyUnavailable(err);
        return;
      }
      onAuthFailure();
    };

    function runAuthProbe() {
      if (cancelled) return;
      api
        .getMe()
        .then((user) => {
          onAuthSuccess(user);
          seedWorkspaceList();
        })
        .catch(handleAuthProbeError);
    }

    const startTokenAuthProbe = () => {
      const token = storage.getItem("multica_token");
      if (!token) {
        onLogout?.();
        useAuthStore.setState({
          user: null,
          isLoading: false,
          authStatus: "unauthenticated",
          authUnavailableSince: null,
        });
        return;
      }

      api.setToken(token);
      runAuthProbe();
    };

    if (cookieAuth) {
      // Cookie mode: the HttpOnly cookie is sent automatically by the browser.
      // Call the API to check if the session is still valid.
      //
      // Seed the workspace list into React Query so the URL-driven layout can
      // resolve the slug without a second fetch. The active workspace itself
      // is derived from the URL by [workspaceSlug]/layout.tsx — no imperative
      // selection here.
      runAuthProbe();
      return () => {
        cancelled = true;
        clearRetry();
      };
    }

    startTokenAuthProbe();
    return () => {
      cancelled = true;
      clearRetry();
    };
  }, []);

  return <>{children}</>;
}
