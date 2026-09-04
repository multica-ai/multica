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
} from "../analytics";
import { configStore } from "../config";
import { workspaceListOptions } from "../workspace/queries";
import { createLogger } from "../logger";
import { clearClientSessionData } from "./session-cleanup";
import { defaultStorage } from "./storage";
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
  storage = defaultStorage,
  cookieAuth,
  identity,
}: {
  children: ReactNode;
  onLogin?: () => void;
  // No `onLogout`: every unauthenticated exit below delegates to the auth
  // store's own teardown, which is where the logout / session-expiry
  // callbacks live.
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
        // Older agent handlers returned success while silently dropping this
        // additive field, so writes stay disabled unless the server declares
        // the persistence contract explicitly.
        configStore
          .getState()
          .setAgentConversationStartersSupported(
            cfg.agent_conversation_starters_supported === true,
          );
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
        expired: false,
      });
      identifyAnalytics(user.id, { email: user.email, name: user.name });
      if (authRecoveryPendingRef.current) {
        authRecoveryPendingRef.current = false;
        // A network-not-ready boot can fail both auth and config requests;
        // successful auth is a strong signal to accelerate config recovery.
        retryConfigRef.current();
      }
    };

    const rejectSession = () => {
      settled = true;
      window.removeEventListener("online", retryNow);
      if (retryTimer !== undefined) clearTimeout(retryTimer);
      // One teardown for every dead session, whether it died at boot or
      // under a running shell. The api client's 401 hook has normally run
      // this already by the time we get here; the action is idempotent.
      useAuthStore.getState().sessionExpired();
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
        // No credential to verify. Same published state as a rejected one,
        // and the same teardown — which on desktop is what keeps a daemon
        // running across the restart after an expiry, instead of the app
        // treating a missing token as if the user had signed out.
        settled = true;
        useAuthStore.getState().sessionExpired();
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

  // A session that ends leaves its whole client side behind: a warmed query
  // cache whose every entry has `staleTime: Infinity`, per-workspace drafts
  // and chat selection, the desktop tab layout. The screen it lands on offers
  // a login form, and the next person to use it is not guaranteed to be the
  // one who was just signed in — on a shared machine a colleague would inherit
  // all of it, and sharing a workspace with them means the tab validator finds
  // nothing to drop. So an expiry erases exactly what a logout erases; only
  // the auth teardown differs (the auth store's `sessionExpired`), plus what
  // belongs to a process outliving the session — Desktop's daemon.
  //
  // Keyed on being unauthenticated rather than on a transition out of
  // `authenticated`, because the case that leaks worst never passes through
  // `authenticated` at all: the app starts, the stale token on disk is
  // rejected at the identity probe, and the previous session's drafts sit
  // there waiting for whoever signs in next.
  //
  // This cannot sign anyone out. Reaching `unauthenticated` IS the decision to
  // end the session, and only two things reach it: a 401 on a credential we
  // presented, or having no credential at all. A client that cannot reach the
  // server — offline, DNS down, 5xx — settles on `recovering` and keeps both
  // its token and everything below (see `attempt`'s catch, which narrows to
  // `ApiError` with status 401 before rejecting anything).
  const authStatus = useAuthStore((state) => state.status);
  const clearedForThisSession = useRef(false);
  useEffect(() => {
    if (authStatus === "authenticated") {
      clearedForThisSession.current = false;
      return;
    }
    if (authStatus !== "unauthenticated" || clearedForThisSession.current) {
      return;
    }
    clearedForThisSession.current = true;
    clearClientSessionData(qc, storage);
  }, [authStatus, qc, storage]);

  return <>{children}</>;
}
