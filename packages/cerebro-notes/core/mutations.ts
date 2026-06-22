import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { artifactKeys, coupledNotesKeys } from "@multica/cerebro-artifacts/core";
import { noteKeys } from "./queries";
import { safeParseNote } from "./types";
import type { CreateNoteInput, NoteVisibility } from "./types";

export function useCreateNote() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async (input: CreateNoteInput) =>
      safeParseNote(await api.createNote(input)),
    onSettled: (_data, _err, input) => {
      qc.invalidateQueries({ queryKey: noteKeys.all(wsId) });
      // FIR-1852: an issue/project-scoped note is now a document on that issue —
      // refresh the issue's document list (and the legacy coupled-notes box) so
      // it appears immediately, exactly like creating a document does.
      if (input?.issue_id) {
        qc.invalidateQueries({
          queryKey: artifactKeys.byIssue(wsId, input.issue_id),
        });
        qc.invalidateQueries({
          queryKey: coupledNotesKeys.forIssue(wsId, input.issue_id),
        });
      }
      if (input?.project_id) {
        qc.invalidateQueries({
          queryKey: artifactKeys.byProject(wsId, input.project_id),
        });
      }
    },
  });
}

export function useUpdateNote() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async ({
      id,
      title,
      body,
    }: {
      id: string;
      title?: string;
      body?: string;
    }) => safeParseNote(await api.updateNote(id, { title, body })),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: noteKeys.all(wsId) });
    },
  });
}

export function useDeleteNote() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteNote(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: noteKeys.all(wsId) });
    },
  });
}

export function useSetNoteVisibility() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      id,
      visibility,
      sharedUserIds,
    }: {
      id: string;
      visibility: NoteVisibility;
      sharedUserIds?: string[];
    }) =>
      api.setNoteVisibility(id, {
        visibility,
        shared_user_ids: sharedUserIds,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: noteKeys.all(wsId) });
    },
  });
}

export function useSetNotePin() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, pinned }: { id: string; pinned: boolean }) =>
      api.setNotePin(id, pinned),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: noteKeys.all(wsId) });
    },
  });
}

// useSetNoteFolder moves a note into a folder (or out of all folders when
// folderId is null). A note is an artifact under the hood, so this reuses the
// shared artifact folder-move endpoint; we just invalidate the notes list so
// the moved note lands in the right folder view (TECH-3637).
export function useSetNoteFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, folderId }: { id: string; folderId: string | null }) =>
      api.moveArtifactToFolder(id, { folder_id: folderId }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: noteKeys.all(wsId) });
    },
  });
}
