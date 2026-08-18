import { useEffect, useState } from "react";
import { Outlet, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import {
  workspaceBySlugOptions,
  workspaceListOptions,
} from "@multica/core/workspace";
import { getCurrentSlug, setCurrentWorkspace } from "@multica/core/platform";
import { isWorkspaceDeletePending } from "@multica/core/workspace/pending-delete";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceSeen } from "@multica/views/workspace/use-workspace-seen";
import { WelcomeAfterOnboarding } from "@multica/views/workspace/welcome-after-onboarding";
import { WorkspacePresencePrefetch } from "@multica/views/layout";
import { SourceBackfillModal } from "@multica/views/onboarding";
import { useTabStore } from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

/**
 * Which mounted layout instance currently owns the platform workspace
 * singleton. Claimed during render (next to the setCurrentWorkspace write it
 * guards) and read by the unmount cleanup, which must not release a singleton
 * a successor has already taken over (MUL-6303 / #7086).
 *
 * Module scope rather than context: the two instances that have to agree here
 * never share a React tree. Desktop mounts exactly one tab at a time and keys
 * the host on the active tab id (tab-content.tsx), so switching or opening a
 * tab tears the whole router subtree down and builds a new one beside it.
 */
let singletonOwner: symbol | null = null;

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
  // Identity for this mounted instance. A Symbol so two layouts can never
  // compare equal — including the incoming and outgoing layout of a
  // same-workspace tab swap, which carry the same slug and workspace id and
  // are therefore indistinguishable by value.
  const [instanceId] = useState(() => Symbol("workspace-route-layout"));
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

  const { data: workspace } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug ?? ""),
    enabled: !!user && !!workspaceSlug,
  });
  // A failed background refetch retains the last authoritative selection.
  // Only undefined means the shared workspace list has never resolved.
  const listReady = workspace !== undefined;

  const { data: wsList } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  // Feed the URL slug into the platform singleton so the API client's
  // X-Workspace-Slug header and persist namespace follow the active tab.
  // setCurrentWorkspace self-dedupes on slug equality.
  //
  // Stays in render (not an effect) on purpose: children mount below this
  // one and their queries fire in effects, which run bottom-up — an effect
  // here would set the header AFTER the first child query already used it.
  //
  // The pending-delete guard exists because this write would otherwise undo
  // the delete flow's own cleanup (MUL-6231 / #7021). useDeleteWorkspace
  // clears the singleton and navigates away, but this layout is subscribed to
  // the overlay store, so opening the new-workspace overlay re-renders it
  // while the deleted workspace is STILL in the list cache (the invalidation
  // refetch is a network round-trip). Without the guard we write the dead slug
  // straight back over the cleanup.
  //
  // Claiming ownership here — in the same render pass, before any cleanup can
  // run — is what makes the unmount release below safe. React renders the
  // incoming tree before it runs the outgoing tree's effect cleanups, so by
  // the time the old layout tries to release, the new one already owns.
  if (workspace && workspaceSlug && !isWorkspaceDeletePending(workspace.id)) {
    singletonOwner = instanceId;
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
    if (!listReady) return;
    if (workspace) return;
    if (hasBeenSeen) return; // active eviction in flight — let the other path win
    if (!wsList) return;
    const validSlugs = new Set(wsList.map((w) => w.slug));
    useTabStore.getState().validateWorkspaceSlugs(validSlugs);
  }, [user, listReady, workspace, hasBeenSeen, wsList]);

  // Release the platform singleton when this layout's workspace stops
  // resolving, and again when the layout unmounts. Nothing else owned that
  // lifecycle: the singleton used to keep pointing at a deleted workspace
  // indefinitely, which is how the shell ended up holding workspace-scoped
  // chrome over a workspace that no longer existed (MUL-6231 / #7021).
  //
  // Both paths check `getCurrentSlug() === workspaceSlug` first. On a
  // workspace switch React renders the incoming layout — which sets the
  // singleton to the NEW slug — before running the outgoing one's cleanup, so
  // an unguarded clear would wipe the workspace context that just arrived.
  useEffect(() => {
    if (!listReady) return;
    if (workspace) return;
    if (getCurrentSlug() !== workspaceSlug) return;
    setCurrentWorkspace(null, null);
  }, [listReady, workspace, workspaceSlug]);

  // The ownership check is what keeps a same-workspace tab swap from wiping
  // the workspace context (MUL-6303 / #7086). The slug check alone cannot see
  // that swap: the "+" button opens /<slug>/issues in the ACTIVE workspace
  // (tab-bar.tsx), so the incoming layout carries the SAME slug, its render
  // write dedupes to a no-op inside setCurrentWorkspace, and the outgoing
  // cleanup then found getCurrentSlug() still equal to its own slug and
  // cleared it. Every workspace-scoped request the new tab fired went out
  // without X-Workspace-Slug and came back 400, and the shell — which gates
  // its chrome on the same singleton — dropped the sidebar with it. Nothing
  // re-ran the render write afterwards, so it stayed broken until relaunch.
  //
  // Both checks still earn their place: ownership covers a successor that
  // shares this slug, and the slug check covers this same instance being
  // re-run for a new slug (the effect's own dependency change), where the
  // singleton has already moved on and must not be clobbered.
  useEffect(() => {
    return () => {
      if (singletonOwner !== instanceId) return;
      if (getCurrentSlug() !== workspaceSlug) return;
      singletonOwner = null;
      setCurrentWorkspace(null, null);
    };
  }, [workspaceSlug, instanceId]);

  if (isAuthLoading) return null;
  if (!user) return null;
  if (!workspaceSlug) return null;
  if (!listReady) return null;
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
