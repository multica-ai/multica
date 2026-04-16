import { useEffect, useRef } from "react";
import { Outlet, useNavigate, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { WorkspaceSlugProvider, paths } from "@multica/core/paths";
import { workspaceBySlugOptions } from "@multica/core/workspace";
import {
  setCurrentWorkspace,
  rehydrateAllWorkspaceStores,
} from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";

/**
 * Desktop equivalent of apps/web/app/[workspaceSlug]/layout.tsx.
 *
 * Reads :workspaceSlug from react-router params, resolves it to a Workspace
 * object via the React Query list cache, and syncs the URL-derived workspace
 * into the platform singleton (slug + UUID). Children (DashboardGuard +
 * dashboard layout) handle auth check, loading, and workspace-not-found.
 */
export function WorkspaceRouteLayout() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>();
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);

  const { data: workspace, isFetched: listFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug ?? ""),
    enabled: !!user && !!workspaceSlug,
  });

  // Render-phase sync (same pattern as web layout).
  const syncedSlugRef = useRef<string | null>(null);
  if (workspace && workspaceSlug && syncedSlugRef.current !== workspaceSlug) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
    rehydrateAllWorkspaceStores();
    syncedSlugRef.current = workspaceSlug;
  }

  // Slug doesn't resolve → onboarding. Skip when user is null.
  useEffect(() => {
    if (!user) return;
    if (listFetched && !workspace) navigate(paths.onboarding(), { replace: true });
  }, [user, listFetched, workspace, navigate]);

  if (isAuthLoading) return null;
  if (!workspaceSlug) return null;

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      <Outlet />
    </WorkspaceSlugProvider>
  );
}
