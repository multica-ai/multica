import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";
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
  // FIR-1621 — null while the comment is an unsent local draft; an RFC3339
  // timestamp once it has been dispatched to the coupled issue/chat agent.
  sent_to_agent_at: z.string().nullable().default(null),
});

export type NoteComment = z.infer<typeof NoteCommentSchema>;

// hasAgentMention — the comment body @-tags at least one agent. Mirrors the
// server's mention parser (`mention://agent/<id>` links). Only tagged comments
// are dispatched to an agent.
export function hasAgentMention(body: string): boolean {
  return /\]\(mention:\/\/agent\//.test(body);
}

// IssueMention — an issue @-tagged inside a comment body. `label` is the
// display text (the issue identifier, e.g. "FIR-123"); `id` is the issue UUID.
export interface IssueMention {
  id: string;
  label: string;
}

// Mirrors the server mention scheme `[FIR-123](mention://issue/<uuid>)`
// (server/internal/util/mention.go). The capture is non-greedy on the label so
// a bracketed label still resolves to the trailing `](mention://issue/` anchor.
const ISSUE_MENTION_RE = /\[(.+?)\]\(mention:\/\/issue\/([0-9a-fA-F-]+)\)/g;

// parseIssueMentions — distinct issues @-tagged in a comment body, first-seen
// order. FIR-1753 — drives the "this comment mentions an issue, link it as a
// reference?" suggestion when the note has no issue coupling yet.
export function parseIssueMentions(body: string): IssueMention[] {
  const seen = new Set<string>();
  const out: IssueMention[] = [];
  for (const m of body.matchAll(ISSUE_MENTION_RE)) {
    const id = m[2];
    const label = m[1];
    if (!id || !label || seen.has(id)) continue;
    seen.add(id);
    out.push({ id, label });
  }
  return out;
}

// isUnsentToAgent — a member-authored comment that @-tags an agent and has not
// yet been dispatched to the coupled destination. A comment that does not tag
// an agent is local note discussion, never sent — so it is not "unsent". Agent
// and suggestion rows are never unsent drafts either. Drives the "unsent
// comments" notice + per-comment badge.
export function isUnsentToAgent(c: NoteComment): boolean {
  return (
    c.author_type === "member" &&
    c.kind === "comment" &&
    !c.sent_to_agent_at &&
    hasAgentMention(c.body)
  );
}

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

// FIR-1621 — send unsent comments to the coupled destination. The server
// echoes the dispatched comments (with their new sent_to_agent_at), how many
// drafts remain, and where they went, so the UI can update without a refetch.
// Every field defaulted (API Response Compatibility rule).
export const SendNoteCommentsResultSchema = z.object({
  sent: NoteCommentListSchema.default([]),
  unsent_remaining: z.number().default(0),
  destination_kind: z.string().default(""),
  destination_ref_id: z.string().default(""),
  agents_triggered: z.number().default(0),
});

export type SendNoteCommentsResult = z.infer<
  typeof SendNoteCommentsResultSchema
>;

export function safeParseSendResult(raw: unknown): SendNoteCommentsResult {
  const r = SendNoteCommentsResultSchema.safeParse(raw);
  if (r.success) return r.data;
  console.warn("[cerebro-notes] send-comments response failed validation", r.error);
  return SendNoteCommentsResultSchema.parse({});
}

// SendNoteCommentsInput drives a send. `commentIds` selects which drafts to
// send (omitted/empty = all unsent). FIR-1753 — `destination` names which
// coupling to send to when the note is linked to more than one issue/chat; it
// is omitted in the common single-coupling case.
export interface SendNoteCommentsInput {
  commentIds?: string[];
  destination?: { object: string; refId: string };
}

// useSendNoteComments dispatches the selected unsent comments (or all unsent,
// when commentIds is omitted/empty) to the note's coupled issue/chat. Sending
// is always an explicit user action — never auto-fired on @-mention.
export function useSendNoteComments(noteId: string) {
  const invalidate = useNoteCommentInvalidate(noteId);
  return useMutation({
    mutationFn: async (input?: SendNoteCommentsInput) =>
      safeParseSendResult(
        await api.sendNoteComments(noteId, {
          comment_ids:
            input?.commentIds && input.commentIds.length
              ? input.commentIds
              : undefined,
          destination_object: input?.destination?.object || undefined,
          destination_ref_id: input?.destination?.refId || undefined,
        }),
      ),
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

// ---------------------------------------------------------------------------
// FIR-2595 Stage 2 — mention access. Before a note/comment mention is saved,
// the composer asks the server which tagged members can't open the note, and —
// if the author confirms — grants them access so the notification is openable.
// Replaces the old silent auto-share (which never opened a folder-gated note).
// Backend: server/internal/cerebro/note/mention_access.go
// ---------------------------------------------------------------------------

// extractMemberMentions pulls the unique member UUIDs out of a note/comment body
// written in the standard mention markdown [@label](mention://member/<uuid>).
// Pure + exported so the composer and its test share one source of truth.
export function extractMemberMentions(body: string): string[] {
  const re = /mention:\/\/member\/([0-9a-fA-F-]{36})/g;
  const seen = new Set<string>();
  for (const m of body.matchAll(re)) {
    if (m[1]) seen.add(m[1]);
  }
  return [...seen];
}

const mentionAccessCheckSchema = z.object({
  no_access: z.array(z.string()).default([]),
});

// checkMentionAccess returns the subset of memberIds who cannot currently open
// the note. Empty in → empty out (no round-trip).
export async function checkMentionAccess(
  noteId: string,
  memberIds: string[],
): Promise<string[]> {
  if (memberIds.length === 0) return [];
  const raw = await api.cerebroRequest<unknown>(
    `/api/notes/${noteId}/mention-access?members=${encodeURIComponent(memberIds.join(","))}`,
  );
  return parseWithFallback(raw, mentionAccessCheckSchema, { no_access: [] }, {
    endpoint: "checkMentionAccess",
  }).no_access;
}

// grantMentionAccess grants each member access so they can open the note (a
// foldered note grants viewer on its folder; a root note gets a note-share).
export async function grantMentionAccess(
  noteId: string,
  memberIds: string[],
): Promise<void> {
  if (memberIds.length === 0) return;
  await api.cerebroRequest(`/api/notes/${noteId}/mention-access`, {
    method: "POST",
    body: JSON.stringify({ member_ids: memberIds }),
  });
}
