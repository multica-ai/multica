"use client";

import { useSearchParams } from "next/navigation";
import { NotesPage } from "@multica/cerebro-notes/views";

export default function Page() {
  const params = useSearchParams();
  return <NotesPage initialNoteId={params.get("note")} />;
}
