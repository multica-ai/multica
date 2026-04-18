import { useEffect } from "react";
import { Outlet, useNavigate, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { WorkspaceSlugProvider, paths } from "@multica/core/paths";
import {
  workspaceBySlugOptions,
  workspaceListOptions,
} from "@multica/core/workspace";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceSeen } from "@multica/views/workspace/use-workspace-seen";
import { useTabStore } from "@/stores/tab-store";

/**
 * Desktop equivalent of apps/web/app/[workspaceSlug]/layout.tsx.
 *
 * Resolves the URL slug → workspace UUID via the React Query list cache
 * (seeded by AuthInitializer). Children do not render until the workspace
 * is fully resolved — useWorkspaceId() inside child pages is therefore
 * guaranteed non-null when called. Two industry-standard identities are
 * kept distinct: slug (URL / browser) and UUID (API / cache keys).
 *
 * Unlike web, desktop never renders a "workspace not available" page: the
 * app has no URL bar and no clickable links from outside the session, so
 * landing on an inaccessible slug can only mean stale state (a persisted
 * tab group for a workspace the current user no longer has access to, or
 * active eviction). Both cases resolve by dropping the stale tab group
 * from the tab store — the TabBar then renders a different workspace or
 * the WindowOverlay takes over (zero valid workspaces).
 */
export function WorkspaceRouteLayout() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>();
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);

  // Workspace routes require auth. If user is unauthenticated, bounce to /login.
  useEffect(() => {
    if (!isAuthLoading && !user) navigate(paths.login(), { replace: true });
  }, [isAuthLoading, user, navigate]);

  const { data: workspace, isFetched: listFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug ?? ""),
    enabled: !!user && !!workspaceSlug,
  });

  const { data: wsList } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  // Feed the URL slug into the platform singleton so the API client's
  // X-Workspace-Slug header and persist namespace follow the active tab.
  // setCurrentWorkspace self-dedupes on slug equality.
  if (workspace && workspaceSlug) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  }

  const hasBeenSeen = useWorkspaceSeen(workspaceSlug, !!workspace);

  // Stale-slug auto-heal: when this tab's slug fails to resolve, drop the
  // whole workspace group from the tab store. Per-workspace tab grouping
  // means the cleanup is a single validator call — the TabContent will
  // unmount this tab (and all siblings in the stale group) once the store
  // updates. We don't navigate this tab's router because the tab's path
  // is scoped to the stale slug; navigating to "/" would create an
  // inconsistent "tab in group X with path /" state.
  useEffect(() => {
    if (!user) return;
    if (!listFetched) return;
    if (workspace) return;
    if (hasBeenSeen) return; // active eviction in flight — let the other path win
    if (!wsList) return;
    const validSlugs = new Set(wsList.map((w) => w.slug));
    useTabStore.getState().validateWorkspaceSlugs(validSlugs);
  }, [user, listFetched, workspace, hasBeenSeen, wsList]);

  if (isAuthLoading) return null;
  if (!workspaceSlug) return null;
  if (!listFetched) return null;
  if (!workspace) return null; // auto-heal effect above handles the cleanup

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      <Outlet />
    </WorkspaceSlugProvider>
  );
}
