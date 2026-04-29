"use client";

import { useEffect, type ReactNode } from "react";
import { useAuthStore } from "./store";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";
import { getAppQueryClient, prepareQueryCacheForLogout } from "@/shared/query";
import { currentUserQueryOptions } from "./queries";
import { workspacesQueryOptions } from "@/features/workspace/queries";
import { setLoggedInCookie, clearLoggedInCookie } from "./auth-cookie";

const logger = createLogger("auth");

/**
 * Initializes auth + workspace state from localStorage on mount.
 * Fires getMe() and listWorkspaces() in parallel when a cached token exists.
 */
export function AuthInitializer({ children }: { children: ReactNode }) {
  useEffect(() => {
    const token = localStorage.getItem("multica_token");
    if (!token) {
      void prepareQueryCacheForLogout(getAppQueryClient());
      clearLoggedInCookie();
      useAuthStore.setState({ isLoading: false });
      return;
    }

    api.setToken(token);
    const wsId = localStorage.getItem("multica_workspace_id");

    // Fire getMe and listWorkspaces in parallel and seed the query cache.
    const queryClient = getAppQueryClient();
    const mePromise = queryClient.fetchQuery(currentUserQueryOptions());
    const wsPromise = queryClient.fetchQuery(workspacesQueryOptions());

    Promise.all([mePromise, wsPromise])
      .then(([user, wsList]) => {
        setLoggedInCookie();
        useAuthStore.setState({ user, isLoading: false });
        useWorkspaceStore.getState().hydrateWorkspace(wsList, wsId);
      })
      .catch((err) => {
        logger.error("auth init failed", err);
        void prepareQueryCacheForLogout(getAppQueryClient());
        api.setToken(null);
        api.setWorkspaceId(null);
        localStorage.removeItem("multica_token");
        localStorage.removeItem("multica_workspace_id");
        clearLoggedInCookie();
        useAuthStore.setState({ user: null, isLoading: false });
      });
  }, []);

  return <>{children}</>;
}
