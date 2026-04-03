"use client";

// MULTICA-LOCAL: Auto-login when no token exists (skip login screen).

import { useEffect, type ReactNode } from "react";
import { useAuthStore } from "./store";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";
import { setLoggedInCookie, clearLoggedInCookie } from "./auth-cookie";

const logger = createLogger("auth");

/**
 * Initializes auth + workspace state from localStorage on mount.
 * In local mode, auto-authenticates if no token exists.
 */
export function AuthInitializer({ children }: { children: ReactNode }) {
  useEffect(() => {
    const token = localStorage.getItem("multica_token");
    if (!token) {
      // MULTICA-LOCAL: Auto-login as the local user.
      autoLogin();
      return;
    }

    api.setToken(token);
    const wsId = localStorage.getItem("multica_workspace_id");

    Promise.all([api.getMe(), api.listWorkspaces()])
      .then(([user, wsList]) => {
        setLoggedInCookie();
        useAuthStore.setState({ user, isLoading: false });
        useWorkspaceStore.getState().hydrateWorkspace(wsList, wsId);
      })
      .catch((err) => {
        logger.error("auth init failed, attempting re-login", err);
        // Token expired or invalid — auto-login again.
        autoLogin();
      });
  }, []);

  return <>{children}</>;
}

async function autoLogin() {
  try {
    const { token, user } = await api.localLogin();
    localStorage.setItem("multica_token", token);
    api.setToken(token);
    setLoggedInCookie();
    useAuthStore.setState({ user, isLoading: false });

    // Hydrate workspaces after login
    const wsList = await api.listWorkspaces();
    useWorkspaceStore.getState().hydrateWorkspace(wsList, null);
  } catch (err) {
    logger.error("auto-login failed", err);
    clearLoggedInCookie();
    useAuthStore.setState({ user: null, isLoading: false });
  }
}
