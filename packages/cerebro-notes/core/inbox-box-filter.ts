// TECH-3690 (Jesper) — pure filter/sort logic for the Notes inbox box. Kept
// headless (no React) so it lives in core and can be unit-tested without a DOM.
import type { Note, NoteVisibility } from "./types";

/** How the Notes box sorts its notes. */
export type NotesBoxSort = "pinned" | "updated" | "created";

export interface NotesBoxSettings {
  limit: number;
  pinnedOnly: boolean;
  sort: NotesBoxSort;
  /** undefined / empty = all visibilities. */
  visibility?: NoteVisibility[];
}

/**
 * Apply the box's settings to a fetched note window: filter by visibility and
 * pinned, sort, then cap to `limit`.
 */
export function applyBoxSettings(notes: Note[], opts: NotesBoxSettings): Note[] {
  let out = notes;
  if (opts.visibility && opts.visibility.length > 0) {
    const set = new Set(opts.visibility);
    out = out.filter((n) => set.has(n.visibility));
  }
  if (opts.pinnedOnly) {
    out = out.filter((n) => n.pinned);
  }
  out = sortNotes(out, opts.sort);
  return out.slice(0, Math.max(0, opts.limit));
}

export function sortNotes(notes: Note[], sort: NotesBoxSort): Note[] {
  const copy = [...notes];
  if (sort === "updated") {
    copy.sort((a, b) => b.updated_at.localeCompare(a.updated_at));
  } else if (sort === "created") {
    copy.sort((a, b) => b.created_at.localeCompare(a.created_at));
  } else {
    // "pinned" — pinned first, then most-recently updated. Mirrors the server's
    // ListNotesForUser ordering so the default matches the full Notes page.
    copy.sort((a, b) => {
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
      return b.updated_at.localeCompare(a.updated_at);
    });
  }
  return copy;
}
