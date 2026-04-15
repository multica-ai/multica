import { useMemo } from "react";
import { CoreProvider } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { WorkspaceIdProvider } from "@multica/core/hooks";
import { storage } from "./storage";
import { API_BASE_URL, APP_URL, WS_URL } from "./constants";
import { apiLogger } from "./logger";

/**
 * Wraps the entire app with CoreProvider (token mode, MMKV storage).
 * Mirrors the desktop app's pattern.
 */
export function AppProviders({ children }: { children: React.ReactNode }) {
  const wsHeaders = useMemo(
    () => (APP_URL ? { Origin: APP_URL } : undefined),
    [],
  );

  return (
    <CoreProvider
      apiBaseUrl={API_BASE_URL}
      apiLogger={apiLogger}
      wsUrl={WS_URL}
      storage={storage}
      queryTokenAuth
      wsHeaders={wsHeaders}
    >
      {children}
    </CoreProvider>
  );
}

/**
 * Provides workspace context to authenticated screens.
 * Must be rendered only when a workspace is selected.
 */
export function WorkspaceProvider({ children }: { children: React.ReactNode }) {
  const workspace = useWorkspaceStore((s) => s.workspace);

  if (!workspace) return null;

  return (
    <WorkspaceIdProvider wsId={workspace.id}>{children}</WorkspaceIdProvider>
  );
}

/** Re-export stores for convenient access */
export { useAuthStore, useWorkspaceStore };
