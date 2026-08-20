"use client";

import { useCallback, useEffect, useRef, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getApi } from "../api";
import { ApiError } from "../api/client";
import { useAuthStore } from "../auth";
import {
  captureSignupSource,
  identify as identifyAnalytics,
  initAnalytics,
  resetAnalytics,
} from "../analytics";
import { configStore } from "../config";
import { workspaceListOptions } from "../workspace/queries";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";
import { setCurrentWorkspace } from "./workspace-storage";
import type { ClientIdentity } from "./types";
import type { StorageAdapter } from "../types/storage";
import type { User } from "../types";

const logger = createLogger("auth");
const RECOVERY_RETRY_DELAYS_MS = [
  1_000,
  2_000,
  4_000,
  8_000,
  16_000,
  30_000,
] as const;

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
  const retryGeneration = useAuthStore((state) => state.retryGeneration);
  const configLoadedRef = useRef(false);
  const configRequestRef = useRef<Promise<boolean> | null>(null);
  const authRecoveryPendingRef = useRef(false);
  const retryConfigRef = useRef<() => void>(() => {});

  const loadConfig = useCallback((): Promise<boolean> => {
    if (configLoadedRef.current) return Promise.resolve(true);
    if (configRequestRef.current) return configRequestRef.current;

    const request = getApi()
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
          // Absent/false on the managed cloud and older servers → section hidden.
          vcsIntegrationAvailable: cfg.vcs_integration_available === true,
        });
        configStore.getState().setDaemonConfig({
          daemonServerUrl: cfg.daemon_server_url,
          daemonAppUrl: cfg.daemon_app_url,
        });
        configStore.getState().setFeatureFlags(cfg.feature_flags);
        configStore.getState().setServerVersion(cfg.server_version);
        // Absent on every server that predates the worktree save gate, which
        // is exactly when the client must not offer the mode (#7113).
        configStore
          .getState()
          .setLocalWorktreeSupported(cfg.local_worktree_supported === true);
        if (cfg.posthog_key) {
          initAnalytics({
            key: cfg.posthog_key,
            host: cfg.posthog_host || "",
            appVersion: identity?.version,
            environment: cfg.analytics_environment,
          });
        }
        configLoadedRef.current = true;
        return true;
      })
      .catch(() => false)
      .finally(() => {
        configRequestRef.current = null;
      });
    configRequestRef.current = request;
    return request;
  }, [identity?.version]);

  useEffect(() => {
    // Stamp attribution before anything else — the signup event (server-side)
    // reads this cookie, so it has to be present before the user hits submit.
    captureSignupSource();

    // Configuration is optional for startup, but an offline boot must still
    // self-heal once connectivity returns. Keep retries rate-limited at the
    // same 30-second ceiling as auth and accelerate them on `online`.
    let cancelled = false;
    let inFlight = false;
    let retryAfterFlight = false;
    let retryIndex = 0;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    const attempt = async () => {
      if (cancelled || configLoadedRef.current) return;
      if (retryTimer !== undefined) {
        clearTimeout(retryTimer);
        retryTimer = undefined;
      }
      if (inFlight) {
        retryAfterFlight = true;
        return;
      }

      inFlight = true;
      const loaded = await loadConfig();
      inFlight = false;
      if (cancelled) return;
      if (loaded) {
        window.removeEventListener("online", retryNow);
        return;
      }
      if (retryAfterFlight) {
        retryAfterFlight = false;
        void attempt();
        return;
      }

      const delay = RECOVERY_RETRY_DELAYS_MS[retryIndex];
      retryIndex = Math.min(
        retryIndex + 1,
        RECOVERY_RETRY_DELAYS_MS.length - 1,
      );
      retryTimer = setTimeout(() => {
        retryTimer = undefined;
        void attempt();
      }, delay);
    };

    const retryNow = () => {
      if (cancelled || configLoadedRef.current) return;
      retryIndex = 0;
      if (retryTimer !== undefined) {
        clearTimeout(retryTimer);
        retryTimer = undefined;
      }
      if (inFlight) {
        retryAfterFlight = true;
        return;
      }
      void attempt();
    };

    retryConfigRef.current = retryNow;
    window.addEventListener("online", retryNow);
    void attempt();

    return () => {
      cancelled = true;
      retryConfigRef.current = () => {};
      window.removeEventListener("online", retryNow);
      if (retryTimer !== undefined) clearTimeout(retryTimer);
    };
  }, [loadConfig]);

  useEffect(() => {
    const api = getApi();
    let cancelled = false;
    let settled = false;
    let inFlight = false;
    let retryAfterFlight = false;
    let retryIndex = 0;
    let loggedTransientFailure = false;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    const onAuthSuccess = (user: User) => {
      onLogin?.();
      useAuthStore.setState({
        user,
        isLoading: false,
        status: "authenticated",
      });
      identifyAnalytics(user.id, { email: user.email, name: user.name });
      if (authRecoveryPendingRef.current) {
        authRecoveryPendingRef.current = false;
        // A network-not-ready boot can fail both auth and config requests;
        // successful auth is a strong signal to accelerate config recovery.
        retryConfigRef.current();
      }
    };

    const onAuthFailure = () => {
      onLogout?.();
      resetAnalytics();
      useAuthStore.setState({
        user: null,
        isLoading: false,
        status: "unauthenticated",
      });
    };

    const rejectSession = () => {
      settled = true;
      window.removeEventListener("online", retryNow);
      if (retryTimer !== undefined) clearTimeout(retryTimer);
      if (!cookieAuth) {
        setCurrentWorkspace(null, null);
      }
      onAuthFailure();
    };

    const scheduleRetry = (attempt: () => Promise<void>) => {
      if (cancelled || settled) return;
      const delay = RECOVERY_RETRY_DELAYS_MS[retryIndex];
      retryIndex = Math.min(
        retryIndex + 1,
        RECOVERY_RETRY_DELAYS_MS.length - 1,
      );
      retryTimer = setTimeout(() => {
        retryTimer = undefined;
        void attempt();
      }, delay);
    };

    const warmWorkspaces = () => {
      void qc.fetchQuery(workspaceListOptions()).catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          rejectSession();
          return;
        }
        // Workspace consumers observe this query directly. React Query owns
        // retries and refetch-on-reconnect independently from identity auth.
        logger.error("workspace bootstrap failed", err);
      });
    };

    const attempt = async (): Promise<void> => {
      if (cancelled || settled || inFlight) return;
      if (retryTimer !== undefined) {
        clearTimeout(retryTimer);
        retryTimer = undefined;
      }
      inFlight = true;

      try {
        const user = await api.getMe();
        if (cancelled) return;

        // Identity verification and workspace loading are separate concerns
        // on both platforms. Publish the verified user immediately; route and
        // shell consumers own the shared workspace query and its recovery.
        settled = true;
        window.removeEventListener("online", retryNow);
        onAuthSuccess(user);
        warmWorkspaces();
      } catch (err) {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          rejectSession();
          return;
        }

        if (!loggedTransientFailure) {
          logger.error("auth init temporarily unavailable", err);
          loggedTransientFailure = true;
        }
        authRecoveryPendingRef.current = true;
        useAuthStore.setState({
          user: null,
          isLoading: true,
          status: "recovering",
        });
        scheduleRetry(attempt);
      } finally {
        inFlight = false;
        if (retryAfterFlight && !cancelled && !settled) {
          retryAfterFlight = false;
          void attempt();
        }
      }
    };

    const retryNow = () => {
      if (cancelled || settled) return;
      retryIndex = 0;
      if (retryTimer !== undefined) {
        clearTimeout(retryTimer);
        retryTimer = undefined;
      }
      if (inFlight) {
        retryAfterFlight = true;
        return;
      }
      void attempt();
    };

    if (!cookieAuth) {
      const token = storage.getItem("multica_token");
      if (!token) {
        settled = true;
        onLogout?.();
        useAuthStore.setState({
          user: null,
          isLoading: false,
          status: "unauthenticated",
        });
      } else {
        api.setToken(token);
        window.addEventListener("online", retryNow);
        void attempt();
      }
    } else {
      window.addEventListener("online", retryNow);
      void attempt();
    }

    return () => {
      cancelled = true;
      window.removeEventListener("online", retryNow);
      if (retryTimer !== undefined) clearTimeout(retryTimer);
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [retryGeneration]);

  return <>{children}</>;
}
