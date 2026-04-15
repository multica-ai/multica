import type { QueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceStore } from "@multica/core/workspace";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";

function workspaceNameForEmail(email: string) {
  const localPart = email.split("@")[0]?.trim();
  return localPart ? `${localPart}'s Workspace` : "My Workspace";
}

function workspaceSlugForEmail(email: string) {
  const base =
    email
      .split("@")[0]
      ?.toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "workspace";

  return `${base}-${Date.now().toString(36)}`;
}

let inflightPromise: Promise<void> | null = null;

export function ensureWorkspaceForUser(email: string, qc: QueryClient): Promise<void> {
  if (inflightPromise) return inflightPromise;

  inflightPromise = (async () => {
    let wsList = await api.listWorkspaces();

    if (wsList.length === 0) {
      const workspace = await api.createWorkspace({
        name: workspaceNameForEmail(email),
        slug: workspaceSlugForEmail(email),
      });
      wsList = [workspace];
    }

    qc.setQueryData<Workspace[]>(workspaceKeys.list(), wsList);
    useWorkspaceStore.getState().hydrateWorkspace(wsList);
  })().finally(() => {
    inflightPromise = null;
  });

  return inflightPromise;
}
