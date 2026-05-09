"use client";

import { use, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { WorkspaceSlugProvider, paths } from "@multica/core/paths";
import { workspaceBySlugOptions } from "@multica/core/workspace";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { NoAccessPage } from "@multica/views/workspace/no-access-page";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { useWorkspaceSeen } from "@multica/views/workspace/use-workspace-seen";

export default function WorkspaceLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = use(params);
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  const router = useRouter();

  // Workspace routes require auth. If user is unauthenticated (initial visit
  // without a session, token expired, another tab logged out, etc.), bounce
  // to /login. Without this, the layout renders null and the user sees a
  // blank page stuck on /{slug}/...
  useEffect(() => {
    if (!isAuthLoading && !user) router.replace(paths.login());
  }, [isAuthLoading, user, router]);

  // Resolve workspace by slug from the React Query list cache.
  // Enabled only when user is authenticated — otherwise the list query isn't seeded.
  const { data: workspace, isFetched: listFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug),
    enabled: !!user,
  });

  // Render-phase sync: feed the URL slug into the platform singleton so
  // the first child query's X-Workspace-Slug header is already correct.
  // setCurrentWorkspace self-dedupes + runs rehydrate as a side effect;
  // safe to call on every render.
  if (workspace) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  }

  // Cookie write (last_workspace_slug) — proxy reads it on next page load
  // to redirect unauthenticated-URL hits to the user's last workspace.
  useEffect(() => {
    if (!workspace || typeof document === "undefined") return;
    const oneYear = 60 * 60 * 24 * 365;
    const secure = location.protocol === "https:" ? "; Secure" : "";
    document.cookie = `last_workspace_slug=${encodeURIComponent(workspaceSlug)}; path=/; max-age=${oneYear}; SameSite=Lax${secure}`;
  }, [workspace, workspaceSlug]);

  // Remember whether this slug has resolved before. Used below to avoid
  // flashing NoAccessPage during active workspace removal (delete, leave,
  // or realtime eviction) — in those cases the caller is navigating away
  // and we just need to hold null briefly.
  const hasBeenSeen = useWorkspaceSeen(workspaceSlug, !!workspace);

  const loadingIndicator = (
    <div className="flex h-svh items-center justify-center">
      <MulticaIcon className="size-6 animate-pulse" />
    </div>
  );

  if (isAuthLoading) return loadingIndicator;
  // Don't render children until workspace is resolved. useWorkspaceId()
  // throws when the list hasn't populated or the slug is unknown — gating
  // here makes that invariant hold for every descendant.
  if (!listFetched) return loadingIndicator;
  if (!workspace) {
    // If we've resolved this slug before in this session, it was just
    // removed from our list (deleted/left/evicted). A navigate is almost
    // certainly in flight — render null to avoid a NoAccessPage flash.
    if (hasBeenSeen) return null;
    // Otherwise: the URL points at a workspace the user never had access
    // to. Show explicit feedback instead of silently redirecting. Doesn't
    // distinguish 404 vs 403 to avoid letting attackers enumerate slugs.
    return <NoAccessPage />;
  }

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      {children}
    </WorkspaceSlugProvider>
  );
}
