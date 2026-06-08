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
// import { defaultStorage } from "./storage";
import { setCurrentWorkspace } from "./workspace-storage";
import type { ClientIdentity } from "./types";
import type { User } from "../types";

const logger = createLogger("auth");

export function AuthInitializer({
  children,
  onLogin,
  onLogout,
  cookieAuth,
  casdoorMode,
  identity,
}: {
  children: ReactNode;
  onLogin?: () => void;
  onLogout?: () => void;
  cookieAuth?: boolean;
  casdoorMode?: boolean;
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
        if (cfg.server_url) configStore.getState().setServerUrl(cfg.server_url);
        configStore.getState().setAuthConfig({
          allowSignup: cfg.allow_signup,
          googleClientId: cfg.google_client_id,
          casdoorEnabled: cfg.casdoor_enabled,
          casdoorLoginUrl: cfg.casdoor_login_url,
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
      onLogin?.();
      useAuthStore.setState({ user, isLoading: false });
      identifyAnalytics(user.id, { email: user.email, name: user.name });
    };

    const onAuthFailure = () => {
      onLogout?.();
      resetAnalytics();
      useAuthStore.setState({ user: null, isLoading: false });
    };

    if (casdoorMode) {
      // Casdoor SSO mode: session lives in the zgsmAdminToken cookie set by
      // the Casdoor OAuth callback. credentials: "include" sends it
      // automatically. No localStorage token needed.
      Promise.all([api.getMe(), api.listWorkspaces()])
        .then(([user, wsList]) => {
          onAuthSuccess(user);
          qc.setQueryData(workspaceKeys.list(), wsList);
        })
        .catch((err) => {
          logger.error("casdoor auth init failed", err);
          onAuthFailure();
        });
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
      Promise.all([api.getMe(), api.listWorkspaces()])
        .then(([user, wsList]) => {
          onAuthSuccess(user);
          qc.setQueryData(workspaceKeys.list(), wsList);
        })
        .catch((err) => {
          logger.error("cookie auth init failed", err);
          onAuthFailure();
        });
      return;
    }

    // Token mode: only accept cs-cloud token from desktop bridge.
    const token =
      typeof window !== "undefined"
        ? (window as unknown as { desktopAPI?: { coStrictToken?: string } }).desktopAPI?.coStrictToken
        : undefined;
    if (!token) {
      logger.error("coStrict token not found — auth failed");
      onAuthFailure();
      return;
    }

    api.setToken(token);

    Promise.all([api.getMe(), api.listWorkspaces()])
      .then(([user, wsList]) => {
        onAuthSuccess(user);
        qc.setQueryData(workspaceKeys.list(), wsList);
      })
      .catch((err) => {
        logger.error("coStrict auth init failed", err);
        api.setToken(null);
        setCurrentWorkspace(null, null);
        onAuthFailure();
      });
  }, []);

  return <>{children}</>;
}
