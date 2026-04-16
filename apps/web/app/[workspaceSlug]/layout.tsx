"use client";

import { use, useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { WorkspaceSlugProvider, paths } from "@multica/core/paths";
import { workspaceBySlugOptions } from "@multica/core/workspace";
import {
  setCurrentWorkspace,
  rehydrateAllWorkspaceStores,
} from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";

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

  // Resolve workspace by slug from the React Query list cache.
  // Enabled only when user is authenticated — otherwise the list query isn't seeded.
  const { data: workspace, isFetched: listFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug),
    enabled: !!user,
  });

  // Render-phase sync: set the current workspace slug + UUID into the
  // platform singleton BEFORE children render. This ensures the first
  // child query's X-Workspace-Slug header is already correct.
  // The ref guard prevents re-running on every render.
  const syncedSlugRef = useRef<string | null>(null);
  if (workspace && syncedSlugRef.current !== workspaceSlug) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
    rehydrateAllWorkspaceStores();
    syncedSlugRef.current = workspaceSlug;
  }

  // Cookie write (last_workspace_slug) — proxy reads it on next page load.
  // ALSO write legacy localStorage["multica_workspace_id"] for forward/back
  // compatibility: if this version ever gets reverted to the pre-refactor
  // build, the legacy code reads that localStorage key to know which
  // workspace to attach to API requests. Without double-writing, a rollback
  // would leave returning users with empty data (API calls would have no
  // X-Workspace-ID header). Forward compatible — new code ignores this key.
  useEffect(() => {
    if (!workspace || typeof document === "undefined") return;
    const oneYear = 60 * 60 * 24 * 365;
    const secure = location.protocol === "https:" ? "; Secure" : "";
    document.cookie = `last_workspace_slug=${encodeURIComponent(workspaceSlug)}; path=/; max-age=${oneYear}; SameSite=Lax${secure}`;
    try {
      localStorage.setItem("multica_workspace_id", workspace.id);
    } catch {
      // localStorage may be unavailable in restricted contexts; non-critical.
    }
  }, [workspace, workspaceSlug]);

  // Slug doesn't match any workspace the user has access to → onboarding.
  // Wait for the list query to settle so we don't bounce on first render.
  // Skip when user is null — DashboardGuard handles the /login redirect.
  useEffect(() => {
    if (!user) return;
    if (listFetched && !workspace) router.replace(paths.onboarding());
  }, [user, listFetched, workspace, router]);

  // Auth still loading → render nothing (let DashboardGuard show its loader).
  if (isAuthLoading) return null;

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      {children}
    </WorkspaceSlugProvider>
  );
}
