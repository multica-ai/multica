"use client";

// FIR-2595: a shareable per-note URL. `/{workspace}/notes/{noteId}` (optionally
// `?comment=<id>`) opens the Notes surface with that note selected — so a link
// pasted into chat or an inbox message opens exactly that note (and comment).
// The list route `/notes` still works; opening a note there silently upgrades
// the address bar to this path (see NotesPage URL sync).

import { useParams, useSearchParams } from "next/navigation";
import { NotesPage } from "@multica/cerebro-notes/views";

export default function Page() {
  const params = useParams<{ noteId: string }>();
  const search = useSearchParams();
  return (
    <NotesPage
      initialNoteId={params.noteId}
      initialCommentId={search.get("comment")}
    />
  );
}
