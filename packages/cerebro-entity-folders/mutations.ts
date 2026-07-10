// FIR-1412: mutations for skill/autopilot folders. Each invalidates the folder
// or membership list so the sidebar + grouping refresh.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { collectionFolderKeys } from "@multica/cerebro-collections";
import {
  createEntityFolder,
  deleteEntityFolder,
  setEntityFolderItem,
  updateEntityFolder,
} from "./api";
import { entityFolderKeys } from "./queries";
import type {
  CreateEntityFolderInput,
  EntityFolderKind,
  SetEntityFolderItemInput,
  UpdateEntityFolderInput,
} from "./types";

// FIR-2688: the Settings → Collections tab reads skill/autopilot folders through
// its own cache key (`collection-folders`), disjoint from `entityFolderKeys`. A
// folder create/rename/move/delete here must also invalidate that key so the
// Collections tree stays aligned with the surface without a hard refresh.
function invalidateEntityCollections(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  kind: EntityFolderKind,
) {
  void qc.invalidateQueries({
    queryKey: collectionFolderKeys.entity(wsId, kind),
  });
}

export function useCreateEntityFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: CreateEntityFolderInput) => createEntityFolder(input),
    onSettled: (_data, _err, input) => {
      void qc.invalidateQueries({
        queryKey: entityFolderKeys.folders(wsId, input.kind),
      });
      invalidateEntityCollections(qc, wsId, input.kind);
    },
  });
}

export function useUpdateEntityFolder(kind: EntityFolderKind) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateEntityFolderInput }) =>
      updateEntityFolder(id, input),
    onSettled: () => {
      void qc.invalidateQueries({
        queryKey: entityFolderKeys.folders(wsId, kind),
      });
      invalidateEntityCollections(qc, wsId, kind);
    },
  });
}

export function useDeleteEntityFolder(kind: EntityFolderKind) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => deleteEntityFolder(id),
    onSettled: () => {
      // Deleting a folder cascades sub-folders away and unfiles its items, so
      // both lists may change.
      void qc.invalidateQueries({
        queryKey: entityFolderKeys.folders(wsId, kind),
      });
      void qc.invalidateQueries({
        queryKey: entityFolderKeys.items(wsId, kind),
      });
      invalidateEntityCollections(qc, wsId, kind);
    },
  });
}

export function useSetEntityFolderItem() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: SetEntityFolderItemInput) => setEntityFolderItem(input),
    onSettled: (_data, _err, input) => {
      void qc.invalidateQueries({
        queryKey: entityFolderKeys.items(wsId, input.kind),
      });
    },
  });
}
