"use client";

import { useParams } from "next/navigation";
import { DocumentViewPage } from "@multica/cerebro-artifacts/views/pages";
import { NoteCommentsPanel } from "@multica/cerebro-notes/views/note-comments-panel";
import { NoteReferencesSection } from "@multica/cerebro-notes/views/note-references";
import { NoteVersionsDialog } from "@multica/cerebro-notes/views/note-versions-dialog";

export default function Page() {
  const params = useParams<{ id: string }>();
  return (
    <DocumentViewPage
      artifactId={params.id}
      // FIR-1621 — inject the Notes comments panel + references picker so
      // documents (not just notes) can be commented on and coupled to an issue.
      // Wired at the app layer to avoid a cerebro-artifacts ↔ cerebro-notes
      // package cycle.
      renderComments={({
        artifactId,
        body,
        isOwner,
        draftQuote,
        activeAnchorId,
        editor,
        onClearDraft,
        onSelectThread,
        onClose,
      }) => (
        <NoteCommentsPanel
          noteId={artifactId}
          noteBody={body}
          isOwner={isOwner}
          draftQuote={draftQuote}
          activeAnchorId={activeAnchorId}
          editor={editor}
          onClearDraft={onClearDraft}
          onSelectThread={onSelectThread}
          onClose={onClose}
        />
      )}
      renderReferences={({ artifactId }) => (
        <NoteReferencesSection noteId={artifactId} />
      )}
      // FIR-2697 — version history for a document reuses the note version dialog;
      // the artifact id is a valid noteId for /api/notes/{id}/versions.
      renderVersions={({ artifactId, open, onOpenChange, onRestored }) => (
        <NoteVersionsDialog
          noteId={artifactId}
          open={open}
          onOpenChange={onOpenChange}
          onRestored={onRestored}
        />
      )}
    />
  );
}
