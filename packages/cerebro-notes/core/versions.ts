import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
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
      // Restore changes both the note body and the version list.
      qc.invalidateQueries({ queryKey: noteKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: noteVersionKeys.byNote(wsId, noteId) });
    },
  });
}
