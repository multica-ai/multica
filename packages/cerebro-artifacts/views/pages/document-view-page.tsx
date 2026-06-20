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
  X,
} from "lucide-react";
import { toast } from "sonner";
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
import { issueDetailOptions } from "@multica/core/issues/queries";
import {
  useDeleteArtifact,
  useUpdateArtifact,
  countMatches,
  replaceAll,
  replaceFirst,
} from "@multica/cerebro-artifacts/core";
import { Input } from "@multica/ui/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { ContentEditor } from "@multica/views/editor";
import { ArtifactContent } from "../components/artifact-content";
import { KindIcon, KIND_LABELS } from "../components/kind-icon";
import { MoveScopeMenu } from "../components/move-scope-menu";
import { DocumentToolsSidebar } from "../components/document-tools-sidebar";
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

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

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

/**
 * The "people + things" line under the title — author and the issues / project
 * the document is connected to. Each is clickable: agent goes to the agent
 * profile (placeholder for now via /agents), members are non-link, issues and
 * projects deep-link.
 */
function ConnectionRow({
  artifact,
}: {
  artifact: import("@multica/core/types").Artifact;
}) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { getActorName, getMemberName } = useActorName();
  // Resolve the issue title / identifier when the doc has any issue link.
  const linkedIssueId = artifact.issue_id ?? artifact.origin_issue_id;
  const { data: linkedIssue } = useQuery({
    ...issueDetailOptions(wsId, linkedIssueId ?? ""),
    enabled: Boolean(wsId && linkedIssueId),
  });
  const showOriginSeparately =
    artifact.origin_issue_id && artifact.origin_issue_id !== artifact.issue_id;
  const showRequesterSeparately =
    artifact.requester_user_id &&
    !(
      artifact.author_type === "member" &&
      artifact.author_id === artifact.requester_user_id
    );

  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
      <span className="flex items-center gap-1.5">
        <ActorAvatar
          actorType={artifact.author_type}
          actorId={artifact.author_id}
          size={18}
        />
        <span className="font-medium">
          {getActorName(artifact.author_type, artifact.author_id)}
        </span>
        {artifact.author_type === "agent" && (
          <Badge variant="outline" className="text-[10px]">
            agent
          </Badge>
        )}
      </span>
      {showRequesterSeparately && artifact.requester_user_id && (
        <span className="flex items-center gap-1.5 text-muted-foreground">
          for{" "}
          <ActorAvatar
            actorType="member"
            actorId={artifact.requester_user_id}
            size={16}
          />
          <span className="font-medium text-foreground">
            {getMemberName(artifact.requester_user_id)}
          </span>
        </span>
      )}
      {artifact.issue_id && (
        <span className="flex items-center gap-1 text-muted-foreground">
          on{" "}
          <a
            href={wsPaths.issueDetail(artifact.issue_id)}
            className="underline hover:text-foreground"
          >
            {linkedIssue?.identifier ?? "issue"}
            {linkedIssue?.title ? ` — ${linkedIssue.title}` : ""}
          </a>
        </span>
      )}
      {artifact.project_id && (
        <span className="flex items-center gap-1 text-muted-foreground">
          on{" "}
          <a
            href={wsPaths.projectDetail(artifact.project_id)}
            className="underline hover:text-foreground"
          >
            project
          </a>
        </span>
      )}
      {showOriginSeparately && artifact.origin_issue_id && (
        <span className="flex items-center gap-1 text-muted-foreground">
          from{" "}
          <a
            href={wsPaths.issueDetail(artifact.origin_issue_id)}
            className="underline hover:text-foreground"
          >
            {linkedIssue?.identifier ?? "issue"}
          </a>
        </span>
      )}
    </div>
  );
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
          className="min-h-[60vh]"
        />
      </div>
    </section>
  );
}

/**
 * Inline find & replace bar for the open note. It works directly on the note's
 * content (the document is inline-edited, so there is no separate edit field):
 * the parent passes the current body, this bar computes matches and hands back
 * the replaced body, which the parent writes into the inline editor + autosaves.
 */
function FindReplaceBar({
  body,
  onReplaceAll,
  onReplaceFirst,
  onClose,
}: {
  body: string;
  onReplaceAll: (newBody: string) => void;
  onReplaceFirst: (newBody: string) => void;
  onClose: () => void;
}) {
  const [find, setFind] = React.useState("");
  const [replacement, setReplacement] = React.useState("");
  const matches = React.useMemo(() => countMatches(body, find), [body, find]);

  const doReplaceAll = () => {
    if (!find) return;
    const { body: next, count } = replaceAll(body, find, replacement);
    if (count === 0) {
      toast.info("No matches to replace.");
      return;
    }
    onReplaceAll(next);
    toast.success(`Replaced ${count} ${count === 1 ? "match" : "matches"}.`);
  };

  const doReplaceFirst = () => {
    if (!find) return;
    const { body: next, replaced } = replaceFirst(body, find, replacement);
    if (!replaced) {
      toast.info("No matches to replace.");
      return;
    }
    onReplaceFirst(next);
  };

  return (
    <div className="mb-3 flex flex-wrap items-center gap-2 rounded-md border bg-muted/30 px-3 py-2">
      <div className="flex items-center gap-1.5">
        <Input
          autoFocus
          value={find}
          onChange={(e) => setFind(e.target.value)}
          placeholder="Search…"
          className="h-8 w-40"
          onKeyDown={(e) => {
            if (e.key === "Escape") onClose();
          }}
        />
        <span className="min-w-14 text-xs text-muted-foreground">
          {find ? `${matches} found` : ""}
        </span>
      </div>
      <Input
        value={replacement}
        onChange={(e) => setReplacement(e.target.value)}
        placeholder="Replace with…"
        className="h-8 w-40"
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
        }}
      />
      <Button
        variant="outline"
        size="sm"
        onClick={doReplaceFirst}
        disabled={matches === 0}
      >
        Replace
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={doReplaceAll}
        disabled={matches === 0}
      >
        Replace all
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className="ml-auto"
        title="Close"
        onClick={onClose}
      >
        <X className="size-4" />
      </Button>
    </div>
  );
}

export function DocumentViewPage({
  artifactId,
  renderComments,
  renderReferences,
}: {
  artifactId: string;
  renderComments?: DocumentCommentsSlot;
  renderReferences?: DocumentReferencesSlot;
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
  const [renaming, setRenaming] = React.useState(false);
  const [titleDraft, setTitleDraft] = React.useState("");
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

  const startRename = () => {
    setTitleDraft(artifact.title);
    setRenaming(true);
  };
  const commitRename = async () => {
    const next = titleDraft.trim();
    if (!next || next === artifact.title) {
      setRenaming(false);
      return;
    }
    await update.mutateAsync({ id: artifact.id, data: { title: next } });
    setRenaming(false);
  };
  const cancelRename = () => {
    setRenaming(false);
    setTitleDraft(artifact.title);
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
          <div className="flex items-center gap-0.5 sm:gap-1">
            <Button
              variant="ghost"
              size="sm"
              className="max-sm:px-2"
              title="Open in new window"
              onClick={() =>
                window.open(window.location.href, "_blank", "noreferrer")
              }
            >
              <ExternalLink className="size-4 sm:mr-1" />
              <span className="hidden sm:inline">Open in new window</span>
            </Button>
            {canDownload && (
              <Button
                variant="ghost"
                size="sm"
                className="max-sm:px-2"
                title="Download"
                onClick={handleDownload}
              >
                <Download className="size-4 sm:mr-1" />
                <span className="hidden sm:inline">Download</span>
              </Button>
            )}
            {commentsEnabled && renderComments && (
              <Button
                variant={showComments ? "secondary" : "ghost"}
                size="sm"
                className="max-sm:px-2"
                title="Comments"
                onClick={() =>
                  showComments ? closeComments() : setShowComments(true)
                }
              >
                <MessageSquare className="size-4 sm:mr-1" />
                <span className="hidden sm:inline">Comments</span>
              </Button>
            )}
            {canEdit && (
              <>
                {artifact.format === "md" && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="max-sm:px-2"
                    title="Find & replace"
                    onClick={() => setFindOpen((v) => !v)}
                  >
                    <Replace className="size-4 sm:mr-1" />
                    <span className="hidden sm:inline">Find &amp; replace</span>
                  </Button>
                )}
                <Button
                  variant="ghost"
                  size="sm"
                  className="max-sm:px-2"
                  title="Rename"
                  onClick={startRename}
                >
                  <Pencil className="size-4 sm:mr-1" />
                  <span className="hidden sm:inline">Rename</span>
                </Button>
                {artifact.format !== "md" && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="max-sm:px-2"
                    title="Edit body"
                    onClick={() =>
                      router.push(wsPaths.documentEdit(artifact.id))
                    }
                  >
                    <Pencil className="size-4 sm:mr-1" />
                    <span className="hidden sm:inline">Edit body</span>
                  </Button>
                )}
                <MoveScopeMenu artifact={artifact} />
                <Button
                  variant="ghost"
                  size="sm"
                  className="max-sm:px-2 text-destructive hover:text-destructive"
                  title="Delete"
                  onClick={() => setConfirmDelete(true)}
                >
                  <Trash2 className="size-4 sm:mr-1" />
                  <span className="hidden sm:inline">Delete</span>
                </Button>
              </>
            )}
          </div>
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

        {renaming ? (
          <Input
            autoFocus
            value={titleDraft}
            onChange={(e) => setTitleDraft(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitRename();
              } else if (e.key === "Escape") {
                e.preventDefault();
                cancelRename();
              }
            }}
            className="h-auto py-1 text-2xl font-semibold leading-tight"
          />
        ) : (
          <h1
            className={
              "text-2xl font-semibold leading-tight" +
              (canEdit
                ? " cursor-text rounded px-1 -mx-1 hover:bg-accent/30"
                : "")
            }
            onClick={canEdit ? startRename : undefined}
            title={canEdit ? "Click to rename" : undefined}
          >
            {artifact.title}
          </h1>
        )}
        <ConnectionRow artifact={artifact} />
        <p className="mt-1 text-xs text-muted-foreground">
          Updated {formatDateTime(artifact.updated_at)}
        </p>

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
