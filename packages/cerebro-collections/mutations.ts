// Mutations for per-folder access grants. A write changes the folder's direct
// grants (Valgt her) and, because grants cascade, the effective grants (Arvet)
// of this folder and its descendants — so we invalidate the whole folder-grant
// prefix rather than a single view key.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { removeFolderGrant, upsertFolderGrant } from "./api";
import { folderGrantKeys } from "./queries";
import type { RemoveFolderGrantInput, UpsertFolderGrantInput } from "./types";

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
