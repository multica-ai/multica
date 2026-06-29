// Mutations for per-folder access grants. A write changes the folder's direct
// grants (Valgt her) and, because grants cascade, the effective grants (Arvet)
// of this folder and its descendants — so we invalidate the whole folder-grant
// prefix rather than a single view key.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  createCollectionFolder,
  createCollectionItem,
  moveCollectionFolder,
  removeFolderGrant,
  removeProjectGrant,
  upsertFolderGrant,
  upsertProjectGrant,
  type CreateCollectionFolderInput,
  type CreateCollectionItemInput,
  type MoveCollectionFolderInput,
} from "./api";
import { collectionFolderKeys, folderGrantKeys, projectGrantKeys } from "./queries";
import type {
  RemoveFolderGrantInput,
  RemoveProjectGrantInput,
  UpsertFolderGrantInput,
  UpsertProjectGrantInput,
} from "./types";

export function useCreateCollectionFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: CreateCollectionFolderInput) =>
      createCollectionFolder(input),
    onSettled: (_data, _err, input) => {
      if (input.surface === "artifact") {
        void qc.invalidateQueries({
          queryKey: collectionFolderKeys.artifact(wsId),
        });
      } else {
        void qc.invalidateQueries({
          queryKey: collectionFolderKeys.entity(wsId, input.kind as "skill" | "autopilot"),
        });
      }
    },
  });
}

export function useCreateCollectionItem() {
  return useMutation({
    mutationFn: (input: CreateCollectionItemInput) => createCollectionItem(input),
  });
}

export function useUpsertFolderGrant() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: UpsertFolderGrantInput) => upsertFolderGrant(input),
    onSettled: (_data, _err, input) => {
      void qc.invalidateQueries({
        queryKey: folderGrantKeys.folder(wsId, input.surface, input.folder_id),
      });
    },
  });
}

export function useRemoveFolderGrant() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: RemoveFolderGrantInput) => removeFolderGrant(input),
    onSettled: (_data, _err, input) => {
      void qc.invalidateQueries({
        queryKey: folderGrantKeys.folder(wsId, input.surface, input.folder_id),
      });
    },
  });
}

export function useUpsertProjectGrant() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: UpsertProjectGrantInput) => upsertProjectGrant(input),
    onSettled: (_data, _err, input) => {
      void qc.invalidateQueries({
        queryKey: projectGrantKeys.project(wsId, input.project_id),
      });
    },
  });
}

export function useRemoveProjectGrant() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: RemoveProjectGrantInput) => removeProjectGrant(input),
    onSettled: (_data, _err, input) => {
      void qc.invalidateQueries({
        queryKey: projectGrantKeys.project(wsId, input.project_id),
      });
    },
  });
}

// Re-parents a folder on the Collections tree. The move changes the folder
// list (new parent_id) and, because grants cascade by ancestry, the effective
// access of the moved subtree — so we invalidate both the folder list for the
// affected surface and the folder-grant prefix for the moved folder.
export function useMoveCollectionFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: MoveCollectionFolderInput) =>
      moveCollectionFolder(input),
    onSettled: (_data, _err, input) => {
      if (input.surface === "artifact") {
        void qc.invalidateQueries({
          queryKey: collectionFolderKeys.artifact(wsId),
        });
      } else if (input.entityKind) {
        void qc.invalidateQueries({
          queryKey: collectionFolderKeys.entity(wsId, input.entityKind),
        });
      }
      void qc.invalidateQueries({
        queryKey: folderGrantKeys.folder(wsId, input.surface, input.folderId),
      });
    },
  });
}
