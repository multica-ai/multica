import { z } from "zod";

// Note visibility — mirrors the CHECK constraint in migration 9073_cerebro_note
// and validVisibility in the Go handler.
export type NoteVisibility = "private" | "shared" | "workspace";

export const VISIBILITY_VALUES: NoteVisibility[] = [
  "private",
  "shared",
  "workspace",
];

// NoteSchema parses the /api/notes wire shape defensively (API Response
// Compatibility rule): every field defaulted, unknown visibility downgraded to
// "private" so a server enum drift never crashes the list.
export const NoteSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  folder_id: z.string().nullable().default(null),
  title: z.string().default(""),
  body: z.string().default(""),
  owner_id: z.string().default(""),
  visibility: z
    .enum(["private", "shared", "workspace"])
    .catch("private")
    .default("private"),
  pinned: z.boolean().default(false),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
});

export type Note = z.infer<typeof NoteSchema>;

export const NoteListSchema = z.array(NoteSchema).catch([]);

export interface CreateNoteInput {
  title?: string;
  body?: string;
  folder_id?: string | null;
  visibility?: NoteVisibility;
}

export interface ListNotesParams {
  q?: string;
  limit?: number;
  offset?: number;
}

// safeParseNotes never throws into the UI: on any shape mismatch it logs and
// returns an empty list (fail-soft), matching parseWithFallback semantics.
export function safeParseNotes(raw: unknown): Note[] {
  const result = NoteListSchema.safeParse(raw);
  if (result.success) return result.data;
  console.warn("[cerebro-notes] notes response failed validation", result.error);
  return [];
}

export function safeParseNote(raw: unknown): Note | null {
  const result = NoteSchema.safeParse(raw);
  if (result.success) return result.data;
  console.warn("[cerebro-notes] note response failed validation", result.error);
  return null;
}

// firstLineTitle derives a display title from a note: its title if set, else the
// first non-empty line of the body (Apple Notes pattern), else "New note".
export function firstLineTitle(note: Pick<Note, "title" | "body">): string {
  const t = note.title.trim();
  if (t) return t;
  const firstLine = note.body
    .split("\n")
    .map((l) => l.replace(/^#+\s*/, "").trim())
    .find((l) => l.length > 0);
  return firstLine || "New note";
}

export const VISIBILITY_LABELS: Record<NoteVisibility, string> = {
  private: "Only you",
  shared: "Selected colleagues",
  workspace: "Whole team",
};
