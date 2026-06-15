import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";

// Wave 3 / G1 (TECH-3556) — comments + suggestions on a note. Mirrors the
// server CommentResponse (server/internal/cerebro/note/comments.go). Every
// field defaulted (API Response Compatibility rule); unknown enum values
// downgrade rather than crash.
export const NoteCommentSchema = z.object({
  id: z.string().default(""),
  note_id: z.string().default(""),
  thread_root_id: z.string().nullable().default(null),
  kind: z.enum(["comment", "suggestion"]).catch("comment").default("comment"),
  body: z.string().default(""),
  anchor_quote: z.string().nullable().default(null),
  anchor_prefix: z.string().nullable().default(null),
  anchor_suffix: z.string().nullable().default(null),
  anchor_start: z.number().nullable().default(null),
  anchor_end: z.number().nullable().default(null),
  suggestion_text: z.string().nullable().default(null),
  suggestion_state: z
    .enum(["pending", "accepted", "rejected"])
    .catch("pending")
    .default("pending"),
  resolved: z.boolean().default(false),
  resolved_by: z.string().nullable().default(null),
  resolved_at: z.string().default(""),
  author_type: z.string().default("member"),
  author_id: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
});

export type NoteComment = z.infer<typeof NoteCommentSchema>;

export const NoteCommentListSchema = z.array(NoteCommentSchema).catch([]);

export function safeParseNoteComments(raw: unknown): NoteComment[] {
  const r = NoteCommentListSchema.safeParse(raw);
  if (r.success) return r.data;
  console.warn("[cerebro-notes] comments response failed validation", r.error);
  return [];
}

// A thread = a root comment plus its replies, oldest-first.
export interface NoteThread {
  root: NoteComment;
  replies: NoteComment[];
}

// buildThreads groups a flat comment list into threads (root + replies),
// preserving server order. Replies whose root is missing are dropped.
export function buildThreads(comments: NoteComment[]): NoteThread[] {
  const roots = comments.filter((c) => !c.thread_root_id);
  const byRoot = new Map<string, NoteComment[]>();
  for (const c of comments) {
    if (c.thread_root_id) {
      const list = byRoot.get(c.thread_root_id) ?? [];
      list.push(c);
      byRoot.set(c.thread_root_id, list);
    }
  }
  return roots.map((root) => ({ root, replies: byRoot.get(root.id) ?? [] }));
}

export interface CreateNoteCommentInput {
  kind?: "comment" | "suggestion";
  body: string;
  thread_root_id?: string;
  anchor_quote?: string;
  anchor_prefix?: string;
  anchor_suffix?: string;
  anchor_start?: number;
  anchor_end?: number;
  suggestion_text?: string;
}

export const noteCommentKeys = {
  byNote: (wsId: string, noteId: string) =>
    ["note-comments", wsId, noteId] as const,
};

export function useNoteComments(noteId: string | null | undefined) {
  const wsId = useWorkspaceId();
  return useQuery({
    queryKey: noteCommentKeys.byNote(wsId, noteId ?? ""),
    queryFn: async () =>
      safeParseNoteComments(await api.listNoteComments(noteId as string)),
    enabled: Boolean(wsId && noteId),
    staleTime: 10 * 1000,
    placeholderData: (prev) => prev,
  });
}

function useNoteCommentInvalidate(noteId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return () =>
    qc.invalidateQueries({ queryKey: noteCommentKeys.byNote(wsId, noteId) });
}

export function useCreateNoteComment(noteId: string) {
  const invalidate = useNoteCommentInvalidate(noteId);
  return useMutation({
    mutationFn: (input: CreateNoteCommentInput) =>
      api.createNoteComment(noteId, input),
    onSettled: invalidate,
  });
}

export function useUpdateNoteComment(noteId: string) {
  const invalidate = useNoteCommentInvalidate(noteId);
  return useMutation({
    mutationFn: ({ commentId, body }: { commentId: string; body: string }) =>
      api.updateNoteComment(noteId, commentId, { body }),
    onSettled: invalidate,
  });
}

export function useDeleteNoteComment(noteId: string) {
  const invalidate = useNoteCommentInvalidate(noteId);
  return useMutation({
    mutationFn: (commentId: string) => api.deleteNoteComment(noteId, commentId),
    onSettled: invalidate,
  });
}

export function useResolveNoteComment(noteId: string) {
  const invalidate = useNoteCommentInvalidate(noteId);
  return useMutation({
    mutationFn: ({
      commentId,
      resolved,
    }: {
      commentId: string;
      resolved: boolean;
    }) => api.resolveNoteComment(noteId, commentId, resolved),
    onSettled: invalidate,
  });
}

// Deciding a suggestion can also change the note body (on accept), so callers
// invalidate the note detail too — handled by the view via onSettled override.
export function useDecideNoteSuggestion(noteId: string) {
  const invalidate = useNoteCommentInvalidate(noteId);
  return useMutation({
    mutationFn: ({
      commentId,
      state,
    }: {
      commentId: string;
      state: "accepted" | "rejected";
    }) => api.decideNoteSuggestion(noteId, commentId, state),
    onSettled: invalidate,
  });
}
