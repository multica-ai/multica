"use client";

import type { ReactNode } from "react";
import { useSyncModelRegistryPricing } from "@multica/core/runtimes";
import { useDashboardGuard } from "./use-dashboard-guard";

interface DashboardGuardProps {
  children: ReactNode;
  /** Rendered when auth or workspace is loading */
  loadingFallback?: ReactNode;
}

/**
 * Shared guard for dashboard layouts.
 *
 * Handles: auth check → workspace check → render children.
 * Both web and desktop layouts compose their own UI structure inside this.
 *
 * WorkspaceIdProvider has been removed — useWorkspaceId() now derives from
 * the URL slug via useCurrentWorkspace(). The guard still gates on workspace
 * being resolved so downstream components can safely call useWorkspaceId().
 */
export function DashboardGuard({
  children,
  loadingFallback = null,
}: DashboardGuardProps) {
  const { user, isLoading, workspace } = useDashboardGuard();
  // CEREBRO-PATCH(dashboard-guard-model-registry-sync): FIR-2698 loads the single-source model registry once the
  // dashboard is reachable so token-cost estimation anywhere in the app
  // (runtime lists, dashboards, agent pages) reads live registry prices
  // instead of a hardcoded table. See packages/core/runtimes/use-sync-model-registry-pricing.ts.
  useSyncModelRegistryPricing(!!user);

  if (isLoading || !workspace) return <>{loadingFallback}</>;
  if (!user) return null;

  return <>{children}</>;
}
