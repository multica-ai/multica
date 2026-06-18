// TECH-3511 — TanStack Query hooks for note types.

import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { artifactKeys } from "./queries";
import {
  createNoteType,
  deleteNoteType,
  listNoteTypes,
  runNoteType,
  updateNoteType,
} from "./note-types-api";
import type { NoteTypeWriteInput } from "./note-types-types";

export const noteTypeKeys = {
  all: (wsId: string) => ["cerebro-note-types", wsId] as const,
};

export function noteTypesOptions(wsId: string) {
  return queryOptions({
    queryKey: noteTypeKeys.all(wsId),
    queryFn: () => listNoteTypes(),
    enabled: Boolean(wsId),
  });
}

export function useNoteTypes() {
  const wsId = useWorkspaceId();
  return useQuery(noteTypesOptions(wsId));
}

export function useCreateNoteType() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: NoteTypeWriteInput) => createNoteType(payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: noteTypeKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: artifactKeys.all(wsId) });
    },
  });
}

export function useUpdateNoteType() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: NoteTypeWriteInput }) =>
      updateNoteType(id, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: noteTypeKeys.all(wsId) });
    },
  });
}

export function useDeleteNoteType() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteNoteType(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: noteTypeKeys.all(wsId) });
    },
  });
}

export function useRunNoteType() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => runNoteType(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: noteTypeKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: artifactKeys.all(wsId) });
      // A materialised note type registers as a Notes-feature note, so refresh
      // the Notes list too — otherwise the freshly-started recurring note isn't
      // in the cache and the Notes surface can't open it (FIR-1460). Raw key
      // mirrors noteKeys.all() in @multica/cerebro-notes (avoids a package dep).
      qc.invalidateQueries({ queryKey: ["notes", wsId] });
    },
  });
}
