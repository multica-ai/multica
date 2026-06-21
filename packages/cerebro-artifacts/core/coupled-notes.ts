import { queryOptions } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";

// FIR-1621 — coupled notes shown on the issue document list. A note can
// reference an issue (the forward note→issue link); this is the reverse read:
// the notes coupled to one issue, fetched via GET /api/notes/by-reference. A
// note IS an artifact (kind='note'), so its id opens in the same full-page note
// editor (documentDetail) the document cards use — no separate note route is
// needed here.
//
// Defensive parse (API Response Compatibility rule): the backend wraps the list
// in `{notes: []}`; on any shape drift this returns [] rather than throwing.

export const CoupledNoteSchema = z.object({
  id: z.string().default(""),
  title: z.string().default(""),
  body: z.string().default(""),
  visibility: z.string().default("private"),
  updated_at: z.string().default(""),
});

export type CoupledNote = z.infer<typeof CoupledNoteSchema>;

export const CoupledNotesListSchema = z.array(CoupledNoteSchema).catch([]);

export function safeParseCoupledNotes(raw: unknown): CoupledNote[] {
  const wrapper =
    raw && typeof raw === "object" && "notes" in raw
      ? (raw as { notes?: unknown }).notes
      : raw;
  const result = CoupledNotesListSchema.safeParse(wrapper ?? []);
  if (result.success) return result.data;
  console.warn(
    "[cerebro-artifacts] coupled-notes response failed validation",
    result.error,
  );
  return [];
}

export const coupledNotesKeys = {
  all: (wsId: string) => ["coupled-notes", wsId] as const,
  forIssue: (wsId: string, issueId: string) =>
    [...coupledNotesKeys.all(wsId), "issue", issueId] as const,
};

export function coupledNotesForIssueOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: coupledNotesKeys.forIssue(wsId, issueId),
    queryFn: async () =>
      safeParseCoupledNotes(await api.listNotesForReference("issue", issueId)),
    enabled: Boolean(wsId && issueId),
    staleTime: 15 * 1000,
  });
}
