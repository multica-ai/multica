import { useEffect } from "react";
import { Outlet, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import {
  workspaceBySlugOptions,
  workspaceListOptions,
} from "@multica/core/workspace";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceSeen } from "@multica/views/workspace/use-workspace-seen";
import { WelcomeAfterOnboarding } from "@multica/views/workspace/welcome-after-onboarding";
import { WorkspacePresencePrefetch } from "@multica/views/layout";
import { SourceBackfillModal } from "@multica/views/onboarding";
import { useTabStore } from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

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
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  // While a WindowOverlay is open (onboarding, accept-invite, new-workspace),
  // the underlying tab is still mounted in the React tree — so this layout
  // and its WelcomeAfterOnboarding Modal would render UNDER the overlay.
  // Because the modal uses a Portal that targets document.body, it ends up
  // rendered LATER in the DOM and visually outranks the overlay's z-50.
  // Suppress the modal whenever any overlay is active; the moment the
  // overlay closes the welcome hook re-evaluates and pops if its store
  // signal is still set.
  const overlayActive = useWindowOverlayStore((s) => s.overlay !== null);

  // Workspace routes require auth. App.tsx renders <DesktopLoginPage>
  // instead of the shell whenever `user` is null, so this tree never mounts
  // unauthenticated — the old in-router bounce to /login was dead defensive
  // code and violated MUL-4741 invariant 1 (only the Coordinator navigates).
  // The `!user` early return below keeps the defense without navigating.

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
  if (!user) return null;
  if (!workspaceSlug) return null;
  if (!listFetched) return null;
  if (!workspace) return null; // auto-heal effect above handles the cleanup

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      <WorkspacePresencePrefetch />
      <Outlet />
      {/* Reads the welcome-store transient signal parked by
       *  OnboardingFlow.handleRuntimeNext. Suppressed while a WindowOverlay
       *  (onboarding / accept-invite / new-workspace) is open so the modal
       *  doesn't portal-jump in front of an active pre-workspace flow.
       *  Once the overlay closes the hook re-evaluates and pops the
       *  Modal — unless the store signal has already been consumed, in
       *  which case the hook renders null. */}
      {!overlayActive && <WelcomeAfterOnboarding />}
      {/* Source-attribution backfill: same Dialog the web shell mounts
       *  inside DashboardLayout. Desktop's WorkspaceRouteLayout doesn't
       *  wrap DashboardLayout, so the modal has to be wired in directly
       *  here. Same overlay-suppression rule as WelcomeAfterOnboarding —
       *  a portal-rendered Dialog at z-50 would otherwise sit above an
       *  active pre-workspace overlay. */}
      {!overlayActive && <SourceBackfillModal />}
    </WorkspaceSlugProvider>
  );
}
