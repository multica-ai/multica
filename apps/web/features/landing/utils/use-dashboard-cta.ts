"use client";

import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceList } from "@multica/core/workspace";
import { paths, resolvePostAuthDestination } from "@multica/core/paths";
import type { Workspace } from "@multica/core/types";
import { toWebPostAuthPath } from "@/platform/web-host-path";

/**
 * While the workspace list is in flight, use the authority-backed Tag entry.
 * It can list/switch an existing VIBES Workspace or create one, and never
 * falls through the retired Next Issues/onboarding recovery path.
 */
const LOADING_FALLBACK_HREF = "/tag/workspaces/new";

export function resolveDashboardCtaHref({
  isAuthenticated,
  workspaceListReady,
  workspaces,
}: {
  isAuthenticated: boolean;
  workspaceListReady: boolean;
  workspaces: Workspace[];
}): string {
  if (!isAuthenticated) return paths.login();
  if (!workspaceListReady) return LOADING_FALLBACK_HREF;
  return toWebPostAuthPath(resolvePostAuthDestination(workspaces));
}

/**
 * Destination for the landing "Dashboard" CTA.
 *
 * These CTAs used to point at `/` and lean on the proxy bouncing logged-in
 * visitors from the root path to their workspace. Once `/` stayed public on the
 * official marketing host, that bounce stopped and the CTA resolved to the page
 * the visitor was already on — a click with no visible effect. Resolve the real
 * destination here instead, through the same resolver
 * `RedirectIfAuthenticated` uses, so "where the dashboard lives" has one source
 * of truth.
 *
 * Shares `workspaceListOptions()`' query key with `RedirectIfAuthenticated`, so
 * on the landing page the list is typically already in flight or cached and this
 * adds no request.
 */
export function useDashboardCtaHref(): string {
  const user = useAuthStore((s) => s.user);

  const { workspaces, ready } = useWorkspaceList({ enabled: !!user });

  return resolveDashboardCtaHref({
    isAuthenticated: !!user,
    workspaceListReady: ready,
    workspaces,
  });
}
