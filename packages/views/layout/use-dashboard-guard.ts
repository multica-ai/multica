"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigationStore } from "@multica/core/navigation";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace";
import { useNavigation } from "../navigation";

/**
 * Auth + workspace gate for the dashboard.
 *
 * Redirect logic:
 *  - Auth still loading → wait
 *  - Not logged in → /login
 *  - Logged in but workspace list not yet loaded → wait (don't bounce prematurely)
 *  - Logged in but URL slug doesn't resolve to any workspace → /workspaces/new
 *
 * We read the workspace list query state directly (rather than relying on
 * useCurrentWorkspace's null return) so we can distinguish "list loading"
 * from "slug not found". Otherwise users could see a transient redirect
 * to /workspaces/new before their workspace list arrives.
 */
export function useDashboardGuard() {
  const { pathname, replace } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useCurrentWorkspace();
  const { isFetched: workspaceListFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      replace(paths.login());
      return;
    }
    // Wait for workspace list to settle before deciding "no workspace".
    if (!workspaceListFetched) return;
    if (!workspace) {
      replace(paths.newWorkspace());
    }
  }, [user, isLoading, workspaceListFetched, workspace, replace]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  return { user, isLoading, workspace };
}
