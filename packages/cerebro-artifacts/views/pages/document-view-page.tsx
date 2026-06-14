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
import { issueDetailOptions } from "@multica/core/issues/queries";
import {
  useDeleteArtifact,
  useUpdateArtifact,
  parseOutline,
  countDocument,
} from "@multica/cerebro-artifacts/core";
import { Input } from "@multica/ui/components/ui/input";
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
import type { Artifact } from "@multica/core/types";

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
  onSave,
  onBodyChange,
}: {
  artifact: Artifact;
  onSave: (body: string) => Promise<void>;
  onBodyChange?: (body: string) => void;
}) {
  const lastSavedRef = React.useRef(artifact.body);
  const savingRef = React.useRef(false);
  const pendingRef = React.useRef<string | null>(null);
  const [status, setStatus] = React.useState<SaveStatus>("idle");

  // Re-baseline when the user navigates to a different document.
  React.useEffect(() => {
    lastSavedRef.current = artifact.body;
    setStatus("idle");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [artifact.id]);

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
              Gemmer…
            </>
          )}
          {status === "saved" && (
            <>
              <Save className="size-3.5" />
              Gemt
            </>
          )}
        </div>
      </div>
      <div className="min-h-[65vh] bg-background px-4 py-4 md:px-6 md:py-5">
        <ContentEditor
          key={artifact.id}
          defaultValue={artifact.body}
          onUpdate={handleUpdate}
          debounceMs={800}
          placeholder="Skriv noget…"
          className="min-h-[60vh]"
        />
      </div>
    </section>
  );
}

/**
 * Outline (heading navigation) + word/character count for the open document.
 * The outline scrolls to the Nth heading element inside `contentRef`, which
 * works for both the live editor and the read-only render (both emit real
 * <h1>–<h6> tags in document order). Hidden when there are fewer than 2
 * headings — a single- or no-heading doc needs no navigator.
 */
function DocumentToolsSidebar({
  body,
  contentRef,
}: {
  body: string;
  contentRef: React.RefObject<HTMLDivElement | null>;
}) {
  const outline = React.useMemo(() => parseOutline(body), [body]);
  const counts = React.useMemo(() => countDocument(body), [body]);

  const scrollToHeading = React.useCallback(
    (index: number) => {
      const root = contentRef.current;
      if (!root) return;
      const headings = root.querySelectorAll("h1, h2, h3, h4, h5, h6");
      const el = headings.item(index);
      if (el) el.scrollIntoView({ behavior: "smooth", block: "start" });
    },
    [contentRef],
  );

  return (
    <aside className="hidden w-56 shrink-0 lg:block">
      <div className="sticky top-4 flex flex-col gap-3">
        {outline.length >= 2 && (
          <nav aria-label="Dokument-oversigt" className="flex flex-col gap-1">
            <div className="px-2 text-xs font-medium text-muted-foreground">
              Oversigt
            </div>
            <ul className="flex flex-col">
              {outline.map((h) => (
                <li key={h.index}>
                  <button
                    type="button"
                    onClick={() => scrollToHeading(h.index)}
                    className="w-full truncate rounded px-2 py-1 text-left text-xs text-muted-foreground hover:bg-accent/40 hover:text-foreground"
                    style={{ paddingLeft: `${0.5 + (h.level - 1) * 0.6}rem` }}
                    title={h.text}
                  >
                    {h.text}
                  </button>
                </li>
              ))}
            </ul>
          </nav>
        )}
        <div className="px-2 text-xs text-muted-foreground">
          {counts.words} ord · {counts.characters} tegn
        </div>
      </div>
    </aside>
  );
}

export function DocumentViewPage({ artifactId }: { artifactId: string }) {
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
  // Body used to drive the outline + word count. Seeded from the loaded doc and
  // kept live by the editor's onBodyChange; only re-seeded when the doc changes
  // (id), so autosave refetches never clobber what the user is typing.
  const contentRef = React.useRef<HTMLDivElement>(null);
  const [liveBody, setLiveBody] = React.useState("");
  React.useEffect(() => {
    setLiveBody(artifact?.body ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [artifact?.id]);

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
            {canEdit && (
              <>
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

        <div className="mt-6 flex gap-6">
          <div ref={contentRef} className="min-w-0 flex-1">
            {artifact.format === "md" && canEdit ? (
              <MarkdownDocumentEditor
                artifact={artifact}
                onSave={handleSaveMarkdownBody}
                onBodyChange={setLiveBody}
              />
            ) : (
              <ArtifactContent artifact={artifact} />
            )}
          </div>
          {artifact.format === "md" && (
            <DocumentToolsSidebar
              body={liveBody || artifact.body}
              contentRef={contentRef}
            />
          )}
        </div>

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
