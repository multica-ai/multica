import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { noteKeys } from "./queries";
import { safeParseNote } from "./types";
import type { CreateNoteInput, NoteVisibility } from "./types";

export function useCreateNote() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async (input: CreateNoteInput) =>
      safeParseNote(await api.createNote(input)),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: noteKeys.all(wsId) });
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
