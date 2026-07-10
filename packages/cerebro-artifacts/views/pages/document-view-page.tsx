"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  Pencil,
  Trash2,
  Download,
  ExternalLink,
  RotateCcw,
  Save,
  Replace,
  MessageSquare,
  History,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { artifactDetailOptions } from "@multica/cerebro-artifacts/core";
import {
  useDeleteArtifact,
  useUpdateArtifact,
} from "@multica/cerebro-artifacts/core";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import { ContentEditor } from "@multica/views/editor";
import { ArtifactContent } from "../components/artifact-content";
import { KindIcon, KIND_LABELS } from "../components/kind-icon";
import { DocumentToolsSidebar } from "../components/document-tools-sidebar";
import { EditableTitle } from "../components/editable-title";
import { EditorActionsMenu } from "../components/editor-actions-menu";
import { EntityMetaHeader } from "../components/entity-meta-header";
import { FindReplaceBar } from "../components/find-replace-bar";
import { FolderSuggestionBanner } from "../components/folder-suggestion-banner";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import type { Artifact } from "@multica/core/types";

// The live editor instance ContentEditor hands back via onEditorReady. Derived
// from ContentEditor's own prop type so this package needs no direct @tiptap
// dependency; the host forwards it to the Notes comments panel, which paints the
// orange comment-anchor highlights over the quoted spans.
type EditorReadySlot = NonNullable<
  React.ComponentProps<typeof ContentEditor>["onEditorReady"]
>;
export type DocumentEditorInstance = Parameters<EditorReadySlot>[0];

// FIR-1621 — the Documents editor reuses the Notes comments panel (a comment now
// attaches to any artifact, not only kind='note'). The panel lives in
// @multica/cerebro-notes, which already depends on this package — so importing it
// here would create a package cycle. Instead the host app injects the panel
// through this render slot (apps/web + apps/desktop both depend on cerebro-notes).
//
// "marker og kommentér": the user selects text in the body, the bubble menu's
// Comment icon fires onCommentOnSelection (→ draftQuote), and the panel anchors
// the new comment to that span. draftQuote/activeAnchorId + the live editor are
// owned by DocumentViewPage and passed through here so the body highlight and the
// panel stay in sync — exactly the Notes editor flow.
export type DocumentCommentsSlot = (opts: {
  artifactId: string;
  body: string;
  isOwner: boolean;
  draftQuote: string | null;
  activeAnchorId: string | null;
  editor: DocumentEditorInstance | null;
  onClearDraft: () => void;
  onSelectThread: (id: string | null) => void;
  onClose: () => void;
}) => React.ReactNode;

// FIR-1621 (2.1) — coupling a document/PDF/file to an issue or chat reuses the
// Notes references picker. Injected as a slot for the same package-cycle reason
// as the comments panel.
export type DocumentReferencesSlot = (opts: {
  artifactId: string;
}) => React.ReactNode;

// FIR-2697 — version history for a document. Injected as a slot (same
// package-cycle reason as the comments panel) so cerebro-artifacts does not need
// to import cerebro-notes. The host renders the shared NoteVersionsDialog with
// the artifact id as noteId — the /api/notes/{id}/versions routes accept a plain
// artifact id, exactly as the comments panel already does.
export type DocumentVersionsSlot = (opts: {
  artifactId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // FIR-2697: the host wires this so a restore re-seeds the inline editor with
  // the restored body — the editor only re-seeds on a doc-id change or an
  // explicit signal, so without this a restore leaves stale text until reload.
  onRestored: (body: string) => void;
}) => React.ReactNode;

function slugifyForFilename(title: string): string {
  return (
    title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 80) || "document"
  );
}

function downloadBlob(content: string, mime: string, filename: string) {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

function canEditArtifact(
  artifact: Artifact,
  userId: string | null,
  role: string | null,
): boolean {
  if (role === "owner" || role === "admin") return true;
  return artifact.author_type === "member" && artifact.author_id === userId;
}

type SaveStatus = "idle" | "saving" | "saved";

/**
 * Google-Docs-style inline editor: the document is directly editable in place
 * and every change autosaves (debounced) — no Save button. A small status line
 * shows "Saving…" / "Saved". The editor mounts once per document (key on id),
 * so an autosave updating artifact.body never remounts it mid-typing.
 */
function MarkdownDocumentEditor({
  artifact,
  value,
  remountToken,
  onSave,
  onBodyChange,
  onEditorReady,
  onCommentOnSelection,
}: {
  artifact: Artifact;
  // Seed content for the editor. Normally the saved body; after a find&replace
  // it is the replaced body, paired with a bumped remountToken to re-seed the
  // inline editor in place.
  value: string;
  remountToken: number;
  onSave: (body: string) => Promise<void>;
  onBodyChange?: (body: string) => void;
  // FIR-1621 — same select-and-comment bridge the Notes editor uses. onEditorReady
  // hands the live editor up so the host can paint comment-anchor highlights;
  // onCommentOnSelection fires from the bubble menu with the selected text so a
  // comment can be anchored to that span.
  onEditorReady?: EditorReadySlot;
  onCommentOnSelection?: (text: string) => void;
}) {
  const lastSavedRef = React.useRef(value);
  const savingRef = React.useRef(false);
  const pendingRef = React.useRef<string | null>(null);
  const [status, setStatus] = React.useState<SaveStatus>("idle");

  // Re-baseline when the document changes (id) or content is re-seeded by a
  // replace (remountToken). The seed `value` is already persisted by the caller,
  // so it counts as the last-saved baseline — no spurious save on remount.
  React.useEffect(() => {
    lastSavedRef.current = value;
    setStatus("idle");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [artifact.id, remountToken]);

  const flush = React.useCallback(
    async (body: string) => {
      if (body === lastSavedRef.current) return;
      if (savingRef.current) {
        pendingRef.current = body;
        return;
      }
      savingRef.current = true;
      setStatus("saving");
      try {
        await onSave(body);
        lastSavedRef.current = body;
      } finally {
        savingRef.current = false;
        const queued = pendingRef.current;
        pendingRef.current = null;
        if (queued !== null && queued !== lastSavedRef.current) {
          void flush(queued);
        } else {
          setStatus("saved");
        }
      }
    },
    [onSave],
  );

  const handleUpdate = React.useCallback(
    (body: string) => {
      onBodyChange?.(body);
      void flush(body);
    },
    [flush, onBodyChange],
  );

  return (
    <section className="overflow-hidden rounded-md border border-border bg-card shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/30 px-3 py-2">
        <div className="text-xs font-medium text-muted-foreground">Note</div>
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          {status === "saving" && (
            <>
              <RotateCcw className="size-3.5 animate-spin" />
              Saving…
            </>
          )}
          {status === "saved" && (
            <>
              <Save className="size-3.5" />
              Saved
            </>
          )}
        </div>
      </div>
      <div className="min-h-[65vh] bg-background px-4 py-4 md:px-6 md:py-5">
        <ContentEditor
          key={`${artifact.id}:${remountToken}`}
          defaultValue={value}
          onUpdate={handleUpdate}
          onEditorReady={onEditorReady}
          onCommentOnSelection={onCommentOnSelection}
          debounceMs={800}
          placeholder="Just start writing…"
          // FIR-1621 — Documents are full-page surfaces, so the editor fills the
          // card. Override the global .rich-text-editor 70ch readability cap
          // (FIR-2114), which otherwise leaves a wide gap on the right here.
          className="min-h-[60vh] !max-w-none"
        />
      </div>
    </section>
  );
}

export function DocumentViewPage({
  artifactId,
  renderComments,
  renderReferences,
  renderVersions,
}: {
  artifactId: string;
  renderComments?: DocumentCommentsSlot;
  renderReferences?: DocumentReferencesSlot;
  renderVersions?: DocumentVersionsSlot;
}) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const { data: artifact, isLoading, isError } = useQuery(
    artifactDetailOptions(wsId, artifactId),
  );
  const { userId, role } = useCurrentMember(wsId);
  const remove = useDeleteArtifact();
  const update = useUpdateArtifact();
  const [confirmDelete, setConfirmDelete] = React.useState(false);
  // Body driving the outline, word count and find&replace. Seeded from the
  // loaded doc and kept live by the editor's onBodyChange; only re-seeded when
  // the doc changes (id), so autosave refetches never clobber what's being typed.
  const contentRef = React.useRef<HTMLDivElement>(null);
  const [docBody, setDocBody] = React.useState("");
  // Bumped on a find&replace so the inline editor re-seeds with the new content.
  const [replaceToken, setReplaceToken] = React.useState(0);
  const [findOpen, setFindOpen] = React.useState(false);
  // FIR-1621 — comments on a document (same panel as notes). Gated by the same
  // flag the Notes editor uses; the panel itself shows the agent-collaboration
  // controls only when cerebro_note_agent_collab is also on.
  const commentsEnabled = useFeatureFlag("cerebro_note_comments");
  const agentCollabEnabled = useFeatureFlag("cerebro_note_agent_collab");
  const [showComments, setShowComments] = React.useState(false);
  // FIR-2697 — version history for this document (reuses the note version engine).
  const versionsEnabled = useFeatureFlag("cerebro_document_versions");
  const [showVersions, setShowVersions] = React.useState(false);
  // FIR-1621 "marker og kommentér" — same select-and-comment state the Notes
  // editor owns: the live editor (for the anchor highlight), the in-progress
  // selection being commented on, and which existing comment is "active".
  const [editor, setEditor] = React.useState<DocumentEditorInstance | null>(
    null,
  );
  const [draftQuote, setDraftQuote] = React.useState<string | null>(null);
  const [activeAnchorId, setActiveAnchorId] = React.useState<string | null>(
    null,
  );
  const isMobile = useIsMobile();

  // Selecting text in the body opens the comments panel with that span pre-filled
  // as the quote; closing clears the draft + active highlight. Mirrors notes-page.
  const startCommentOnSelection = React.useCallback((text: string) => {
    if (!text) return;
    setDraftQuote(text);
    setActiveAnchorId(null);
    setShowComments(true);
  }, []);
  const closeComments = React.useCallback(() => {
    setShowComments(false);
    setDraftQuote(null);
    setActiveAnchorId(null);
  }, []);

  React.useEffect(() => {
    setShowComments(false);
    setShowVersions(false);
    setDraftQuote(null);
    setActiveAnchorId(null);
    setEditor(null);
  }, [artifact?.id]);
  React.useEffect(() => {
    setDocBody(artifact?.body ?? "");
    setReplaceToken(0);
    setFindOpen(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [artifact?.id]);

  // Cmd/Ctrl+F opens the inline find&replace bar for editable markdown notes.
  const isEditableMarkdown = artifact?.format === "md";
  React.useEffect(() => {
    if (!isEditableMarkdown) return;
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "f") {
        e.preventDefault();
        setFindOpen(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [isEditableMarkdown]);

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-12 shrink-0 items-center border-b px-4">
          <MobileSidebarTrigger className="mr-0" />
        </div>
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          Loading…
        </div>
      </div>
    );
  }
  if (isError || !artifact) {
    return (
      <div className="h-full overflow-y-auto">
        <div className="mx-auto w-full max-w-3xl px-4 py-4 md:px-8 md:py-6">
          <div className="mb-3 flex items-center gap-2">
            <MobileSidebarTrigger className="mr-0" />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => router.push(wsPaths.documents())}
            >
              <ArrowLeft className="mr-1 size-4" />
              <span className="hidden sm:inline">Documents</span>
            </Button>
          </div>
          <p className="mt-4 text-sm text-muted-foreground">
            This document is not available.
          </p>
        </div>
      </div>
    );
  }

  const canEdit = canEditArtifact(artifact, userId, role);

  const handleDelete = async () => {
    await remove.mutateAsync(artifact);
    setConfirmDelete(false);
    router.push(wsPaths.documents());
  };

  const handleDownload = () => {
    const slug = slugifyForFilename(artifact.title);
    if (artifact.format === "pdf") {
      if (artifact.file_url) {
        window.open(artifact.file_url, "_blank", "noreferrer");
      }
      return;
    }
    if (artifact.format === "html") {
      downloadBlob(artifact.body, "text/html;charset=utf-8", `${slug}.html`);
      return;
    }
    downloadBlob(artifact.body, "text/markdown;charset=utf-8", `${slug}.md`);
  };

  const canDownload =
    (artifact.format === "pdf" && Boolean(artifact.file_url)) ||
    ((artifact.format === "html" || artifact.format === "md") &&
      Boolean(artifact.body));

  const handleSaveMarkdownBody = async (body: string) => {
    await update.mutateAsync({ id: artifact.id, data: { body } });
  };

  // Apply a find&replace result: re-seed the inline editor with the new content
  // (bump the remount token), update the tools body, and persist.
  const applyReplacedBody = (newBody: string) => {
    setDocBody(newBody);
    setReplaceToken((t) => t + 1);
    void handleSaveMarkdownBody(newBody);
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-7xl px-4 py-4 md:px-8 md:py-6">
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <MobileSidebarTrigger className="mr-0" />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => router.push(wsPaths.documents())}
            >
              <ArrowLeft className="mr-1 size-4" />
              <span className="hidden sm:inline">Documents</span>
            </Button>
          </div>
          {/* All actions live behind one shared "⋯" menu — the same component
              the Notes view uses, so documents and notes look identical
              (FIR-1647, request 5). */}
          <EditorActionsMenu
            triggerLabel="Document actions"
            items={[
              {
                key: "open-new-window",
                label: "Open in new window",
                icon: ExternalLink,
                onSelect: () =>
                  window.open(window.location.href, "_blank", "noreferrer"),
              },
              canDownload && {
                key: "download",
                label: "Download",
                icon: Download,
                onSelect: handleDownload,
              },
              commentsEnabled &&
                renderComments && {
                  key: "comments",
                  label: "Comments",
                  icon: MessageSquare,
                  onSelect: () =>
                    showComments ? closeComments() : setShowComments(true),
                },
              versionsEnabled &&
                renderVersions && {
                  key: "versions",
                  label: "Version history",
                  icon: History,
                  onSelect: () => setShowVersions(true),
                },
              canEdit &&
                artifact.format === "md" && {
                  key: "find-replace",
                  label: "Find & replace",
                  icon: Replace,
                  onSelect: () => setFindOpen((v) => !v),
                },
              canEdit &&
                artifact.format !== "md" && {
                  key: "edit-body",
                  label: "Edit body",
                  icon: Pencil,
                  onSelect: () => router.push(wsPaths.documentEdit(artifact.id)),
                },
              canEdit && {
                key: "delete",
                label: "Delete",
                icon: Trash2,
                destructive: true,
                separatorBefore: true,
                onSelect: () => setConfirmDelete(true),
              },
            ]}
          />
        </div>

        <div className="mb-4 flex items-center gap-2">
          <KindIcon
            kind={artifact.kind}
            className="size-4 text-muted-foreground"
          />
          <Badge variant="secondary" className="text-xs font-normal">
            {KIND_LABELS[artifact.kind]}
          </Badge>
          <Badge variant="outline" className="text-xs uppercase">
            {artifact.format}
          </Badge>
          {artifact.author_type === "agent" && (
            <Badge variant="outline" className="text-xs">
              agent
            </Badge>
          )}
        </div>

        <EditableTitle
          value={artifact.title}
          onSave={(next) =>
            update.mutate({ id: artifact.id, data: { title: next } })
          }
          readOnly={!canEdit}
          allowEmpty={false}
          className="font-semibold"
        />
        <EntityMetaHeader
          authorType={artifact.author_type}
          authorId={artifact.author_id}
          updatedAt={artifact.updated_at}
          requesterUserId={artifact.requester_user_id}
          issueId={artifact.issue_id}
          projectId={artifact.project_id}
          originIssueId={artifact.origin_issue_id}
        />

        {/* FIR-2697 part 2 — a pending agent folder suggestion for this
            document. Notes render the same banner with surface='note'. */}
        <FolderSuggestionBanner artifactId={artifact.id} canResolve={canEdit} />

        {/* FIR-1621 (2.1) — couple this document/PDF/file to an issue or chat,
            so its comments can be sent to that destination's agent. Same picker
            as notes; available for every document kind. */}
        {agentCollabEnabled && renderReferences && (
          <div className="mt-3 max-w-xl">
            {renderReferences({ artifactId: artifact.id })}
          </div>
        )}

        <div className="mt-6 flex gap-6">
          <div className="min-w-0 flex-1">
            {artifact.format === "md" && canEdit && findOpen && (
              <FindReplaceBar
                body={docBody || artifact.body}
                onReplaceAll={applyReplacedBody}
                onReplaceFirst={applyReplacedBody}
                onClose={() => setFindOpen(false)}
              />
            )}
            <div ref={contentRef}>
              {artifact.format === "md" && canEdit ? (
                <MarkdownDocumentEditor
                  artifact={artifact}
                  value={docBody || artifact.body}
                  remountToken={replaceToken}
                  onSave={handleSaveMarkdownBody}
                  onBodyChange={setDocBody}
                  onEditorReady={commentsEnabled ? setEditor : undefined}
                  onCommentOnSelection={
                    commentsEnabled ? startCommentOnSelection : undefined
                  }
                />
              ) : (
                <ArtifactContent artifact={artifact} />
              )}
            </div>
          </div>
          {/* Desktop: comments as an inline side rail (FIR-1621), available for
              every document kind — not just markdown — so a PDF or uploaded file
              can be commented on too. Mobile uses a Sheet instead (below). */}
          {commentsEnabled && renderComments && showComments && !isMobile ? (
            <div className="w-80 shrink-0 border-l">
              {renderComments({
                artifactId: artifact.id,
                body: docBody || artifact.body,
                isOwner: canEdit,
                draftQuote,
                activeAnchorId,
                editor: isEditableMarkdown ? editor : null,
                onClearDraft: () => setDraftQuote(null),
                onSelectThread: (id) => {
                  setDraftQuote(null);
                  setActiveAnchorId(id);
                },
                onClose: closeComments,
              })}
            </div>
          ) : (
            artifact.format === "md" && (
              <DocumentToolsSidebar
                body={docBody || artifact.body}
                contentRef={contentRef}
              />
            )
          )}
        </div>

        {/* Mobile: there's no room for a side rail, so comments open as a
            near-full-width sheet (mirrors the Notes editor, FIR-1621). */}
        {commentsEnabled && renderComments && isMobile && (
          <Sheet
            open={showComments}
            onOpenChange={(o) => {
              if (!o) closeComments();
            }}
          >
            <SheetContent
              side="right"
              showCloseButton={false}
              className="flex flex-col p-0 data-[side=right]:w-[94vw]"
            >
              <SheetHeader className="sr-only">
                <SheetTitle>Comments</SheetTitle>
              </SheetHeader>
              {renderComments({
                artifactId: artifact.id,
                body: docBody || artifact.body,
                isOwner: canEdit,
                draftQuote,
                activeAnchorId,
                editor: isEditableMarkdown ? editor : null,
                onClearDraft: () => setDraftQuote(null),
                onSelectThread: (id) => {
                  setDraftQuote(null);
                  setActiveAnchorId(id);
                },
                onClose: closeComments,
              })}
            </SheetContent>
          </Sheet>
        )}

        {versionsEnabled && renderVersions &&
          renderVersions({
            artifactId: artifact.id,
            open: showVersions,
            onOpenChange: setShowVersions,
            // Re-seed the inline editor with the restored content (same signal
            // path as find&replace) so the restore shows live, not after reload.
            onRestored: (body) => {
              setDocBody(body);
              setReplaceToken((t) => t + 1);
            },
          })}

        <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete document?</AlertDialogTitle>
              <AlertDialogDescription>
                This permanently removes &ldquo;{artifact.title}&rdquo;. Cannot
                be undone.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                onClick={handleDelete}
                disabled={remove.isPending}
              >
                {remove.isPending ? "Deleting…" : "Delete"}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </div>
  );
}
