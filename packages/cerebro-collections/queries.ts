// React Query options + keys for per-folder access grants.

import { queryOptions } from "@tanstack/react-query";
import {
  fetchArtifactCollectionFolders,
  fetchEntityCollectionFolders,
  fetchFolderGrants,
  type GrantView,
} from "./api";
import type { GrantSurface } from "./types";

export const folderGrantKeys = {
  // One key per (folder, view) so the Valgt her / Arvet tabs cache
  // independently and a write invalidates only what changed.
  grants: (
    wsId: string,
    surface: GrantSurface,
    folderId: string,
    view: GrantView,
  ) => ["folder-grants", wsId, surface, folderId, view] as const,
  // Prefix that matches both views of a folder, for blanket invalidation.
  folder: (wsId: string, surface: GrantSurface, folderId: string) =>
    ["folder-grants", wsId, surface, folderId] as const,
};

export function folderGrantsOptions(
  wsId: string,
  surface: GrantSurface,
  folderId: string,
  view: GrantView,
  opts?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: folderGrantKeys.grants(wsId, surface, folderId, view),
    queryFn: () => fetchFolderGrants(surface, folderId, view),
    enabled: Boolean(wsId && folderId) && (opts?.enabled ?? true),
    staleTime: 30_000,
  });
}

// Folder lists for the Settings → Collections page.
export const collectionFolderKeys = {
  artifact: (wsId: string) => ["collection-folders", wsId, "artifact"] as const,
  entity: (wsId: string, kind: "skill" | "autopilot") =>
    ["collection-folders", wsId, "entity", kind] as const,
};

export function artifactCollectionFoldersOptions(wsId: string) {
  return queryOptions({
    queryKey: collectionFolderKeys.artifact(wsId),
    queryFn: () => fetchArtifactCollectionFolders(),
    enabled: Boolean(wsId),
    staleTime: 30_000,
  });
}

export function entityCollectionFoldersOptions(
  wsId: string,
  kind: "skill" | "autopilot",
) {
  return queryOptions({
    queryKey: collectionFolderKeys.entity(wsId, kind),
    queryFn: () => fetchEntityCollectionFolders(kind),
    enabled: Boolean(wsId),
    staleTime: 30_000,
  });
}
