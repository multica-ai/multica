// FIR-1412: React Query options + keys for skill/autopilot folders.

import { queryOptions } from "@tanstack/react-query";
import { fetchEntityFolders, fetchEntityFolderItems } from "./api";
import type { EntityFolderKind } from "./types";

export const entityFolderKeys = {
  folders: (wsId: string, kind: EntityFolderKind) =>
    ["entity-folders", wsId, kind] as const,
  items: (wsId: string, kind: EntityFolderKind) =>
    ["entity-folder-items", wsId, kind] as const,
};

export function entityFoldersOptions(wsId: string, kind: EntityFolderKind) {
  return queryOptions({
    queryKey: entityFolderKeys.folders(wsId, kind),
    queryFn: () => fetchEntityFolders(kind),
    enabled: Boolean(wsId),
    staleTime: 30_000,
  });
}

export function entityFolderItemsOptions(wsId: string, kind: EntityFolderKind) {
  return queryOptions({
    queryKey: entityFolderKeys.items(wsId, kind),
    queryFn: () => fetchEntityFolderItems(kind),
    enabled: Boolean(wsId),
    staleTime: 30_000,
  });
}
