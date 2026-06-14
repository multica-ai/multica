import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";

// NoteReference parses the /api/notes/{id}/references wire shape defensively
// (API Response Compatibility rule). Mirrors the server NoteReferenceResponse
// (server/internal/cerebro/note/references.go) and the IssueReference shape in
// @multica/cerebro-references — keep the three in sync. Every field is
// defaulted so a server drift never crashes the list.
export const NoteReferenceSchema = z.object({
  id: z.string().default(""),
  note_id: z.string().default(""),
  object: z.string().default(""),
  type: z.string().nullable().default(null),
  ref_id: z.string().default(""),
  label: z.string().nullable().default(null),
  url: z.string().nullable().default(null),
  metadata: z.record(z.string(), z.unknown()).default({}),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
});

export type NoteReference = z.infer<typeof NoteReferenceSchema>;

export const NoteReferenceListSchema = z.array(NoteReferenceSchema).catch([]);

// CreateNoteReferenceInput is the POST body. `object` + `ref_id` are required;
// everything else is optional. Designed so adding non-issue object types later
// is trivial — only `object`/`ref_id`/`label`/`url` change per kind.
export interface CreateNoteReferenceInput {
  object: string;
  type?: string | null;
  ref_id: string;
  label?: string | null;
  url?: string | null;
  metadata?: Record<string, unknown>;
}

// The backend wraps the list in `{references: []}`. safeParseNoteReferences
// never throws into the UI: on any shape mismatch it logs and returns [].
export function safeParseNoteReferences(raw: unknown): NoteReference[] {
  const wrapper =
    raw && typeof raw === "object" && "references" in raw
      ? (raw as { references?: unknown }).references
      : raw;
  const result = NoteReferenceListSchema.safeParse(wrapper ?? []);
  if (result.success) return result.data;
  console.warn(
    "[cerebro-notes] note references response failed validation",
    result.error,
  );
  return [];
}

// Query keys, workspace + note scoped so switching workspaces or notes swaps
// the cache slot.
export const noteReferenceKeys = {
  all: (wsId: string) => ["note-references", wsId] as const,
  byNote: (wsId: string, noteId: string) =>
    [...noteReferenceKeys.all(wsId), noteId] as const,
};

export function useNoteReferences(noteId: string | null | undefined) {
  const wsId = useWorkspaceId();
  return useQuery({
    queryKey: noteReferenceKeys.byNote(wsId, noteId ?? ""),
    queryFn: async () =>
      safeParseNoteReferences(await api.listNoteReferences(noteId as string)),
    enabled: Boolean(wsId && noteId),
    staleTime: 15 * 1000,
    placeholderData: (prev) => prev,
  });
}

export function useAddNoteReference(noteId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: CreateNoteReferenceInput) =>
      api.createNoteReference(noteId, input),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: noteReferenceKeys.byNote(wsId, noteId),
      });
    },
  });
}

export function useDeleteNoteReference(noteId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (refId: string) => api.deleteNoteReference(noteId, refId),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: noteReferenceKeys.byNote(wsId, noteId),
      });
    },
  });
}
