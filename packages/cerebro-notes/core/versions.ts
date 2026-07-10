import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { artifactKeys } from "@multica/cerebro-artifacts/core";
import { noteKeys } from "./queries";
import { safeParseNote } from "./types";

// Wave 3 / G2 (TECH-3556) — version history. Mirrors the server
// VersionResponse (server/internal/cerebro/note/versions.go).
export const NoteVersionSchema = z.object({
  id: z.string().default(""),
  note_id: z.string().default(""),
  version_no: z.number().default(0),
  title: z.string().default(""),
  body: z.string().default(""),
  byte_size: z.number().default(0),
  reason: z.enum(["edit", "manual", "restore"]).catch("edit").default("edit"),
  label: z.string().default(""),
  author_type: z.string().default("member"),
  author_id: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
});

export type NoteVersion = z.infer<typeof NoteVersionSchema>;

export const NoteVersionListSchema = z.array(NoteVersionSchema).catch([]);

export function safeParseNoteVersions(raw: unknown): NoteVersion[] {
  const r = NoteVersionListSchema.safeParse(raw);
  if (r.success) return r.data;
  console.warn("[cerebro-notes] versions response failed validation", r.error);
  return [];
}

export const noteVersionKeys = {
  byNote: (wsId: string, noteId: string) =>
    ["note-versions", wsId, noteId] as const,
};

// FIR-2697: the query keys a restore must invalidate. A restore changes the
// record's body AND the version list, and the same record is read through two
// caches: the Notes editor reads it via noteKeys, the Documents editor reads it
// via artifactKeys.detail. Invalidating only the note keys left a restored
// document showing stale body text until a manual reload. Keeping the list here
// (and asserting it in a test) stops that alignment gap from regressing. noteId
// is the artifact id for a document; the artifact keys are a harmless no-op for
// a pure personal note.
export function restoreVersionInvalidationKeys(wsId: string, noteId: string) {
  return [
    noteKeys.all(wsId),
    noteVersionKeys.byNote(wsId, noteId),
    artifactKeys.detail(wsId, noteId),
    artifactKeys.all(wsId),
  ];
}

export function useNoteVersions(
  noteId: string | null | undefined,
  enabled = true,
) {
  const wsId = useWorkspaceId();
  return useQuery({
    queryKey: noteVersionKeys.byNote(wsId, noteId ?? ""),
    queryFn: async () =>
      safeParseNoteVersions(await api.listNoteVersions(noteId as string)),
    enabled: Boolean(wsId && noteId && enabled),
    placeholderData: (prev) => prev,
  });
}

export function useSaveNoteVersion(noteId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (label?: string) => api.saveNoteVersion(noteId, label),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: noteVersionKeys.byNote(wsId, noteId) }),
  });
}

export function useRestoreNoteVersion(noteId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async (versionId: string) =>
      safeParseNote(await api.restoreNoteVersion(noteId, versionId)),
    onSettled: () => {
      for (const queryKey of restoreVersionInvalidationKeys(wsId, noteId)) {
        qc.invalidateQueries({ queryKey });
      }
    },
  });
}
