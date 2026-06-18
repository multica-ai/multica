// FIR-1412: mutations for skill/autopilot folders. Each invalidates the folder
// or membership list so the sidebar + grouping refresh.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
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

export function useCreateEntityFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: CreateEntityFolderInput) => createEntityFolder(input),
    onSettled: (_data, _err, input) => {
      void qc.invalidateQueries({
        queryKey: entityFolderKeys.folders(wsId, input.kind),
      });
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
