import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { create } from "zustand";
import { useAuthStore } from "@multica/core/auth";
import { isReservedSlug, useHasOnboarded } from "@multica/core/paths";
import { defaultStorage, setCurrentWorkspace } from "@multica/core/platform";
import type { Workspace } from "@multica/core/types";
import { workspaceListOptions } from "@multica/core/workspace/queries";

// Web and Desktop take the workspace from the URL. The app has no URL, so it
// remembers the last one the user was in and falls back to the first.
const LAST_WORKSPACE_KEY = "multica_mobile_last_workspace";

export function chooseWorkspace(
  workspaces: readonly Workspace[],
  preferredSlug: string | null,
): Workspace | null {
  return workspaces.find((ws) => ws.slug === preferredSlug) ?? workspaces[0] ?? null;
}

/** The workspace a core path such as `/acme/issues` points into, if any. */
export function workspaceSlugFromPath(path: string): string | null {
  const slug = path.split(/[?#]/, 1)[0]?.split("/").find(Boolean);
  return slug && !isReservedSlug(slug) ? slug : null;
}

interface WorkspacePreference {
  slug: string | null;
}

const useWorkspacePreference = create<WorkspacePreference>(() => ({
  slug: defaultStorage.getItem(LAST_WORKSPACE_KEY),
}));

export function selectWorkspace(slug: string | null): void {
  if (slug) defaultStorage.setItem(LAST_WORKSPACE_KEY, slug);
  else defaultStorage.removeItem(LAST_WORKSPACE_KEY);
  useWorkspacePreference.setState({ slug });
}

/** Follows core to the workspace it chose after the current one was deleted or the user was removed from it. */
export function handleWorkspaceLost(destination: string): void {
  selectWorkspace(workspaceSlugFromPath(destination));
}

export type WorkspaceSelection =
  | { status: "signed-out" }
  | { status: "loading" }
  | { status: "error"; retry: () => void }
  | { status: "onboarding" }
  | { status: "no-workspace" }
  | { status: "ready"; workspace: Workspace; workspaces: Workspace[] };

/**
 * Resolves which workspace the app shows and publishes it to core, which
 * scopes requests, the realtime connection and persisted stores by it.
 * Mount once, under CoreProvider.
 */
export function useWorkspaceSelection(): WorkspaceSelection {
  const signedIn = useAuthStore((state) => state.user !== null);
  const hasOnboarded = useHasOnboarded();
  const preferredSlug = useWorkspacePreference((state) => state.slug);
  const list = useQuery({ ...workspaceListOptions(), enabled: signedIn && hasOnboarded });
  const workspace = list.data ? chooseWorkspace(list.data, preferredSlug) : null;
  const loaded = list.data !== undefined;

  useEffect(() => {
    if (!signedIn || !hasOnboarded || !loaded) return;
    setCurrentWorkspace(workspace?.slug ?? null, workspace?.id ?? null);
  }, [signedIn, hasOnboarded, loaded, workspace?.slug, workspace?.id]);

  if (!signedIn) return { status: "signed-out" };
  if (!hasOnboarded) return { status: "onboarding" };
  if (list.isError && !loaded) return { status: "error", retry: () => void list.refetch() };
  if (!list.data) return { status: "loading" };
  if (!workspace) return { status: "no-workspace" };
  return { status: "ready", workspace, workspaces: list.data };
}
