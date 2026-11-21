"use client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  History,
  Link as LinkIcon,
  MessageSquare,
  MoreHorizontal,
  FolderInput,
  Trash2,
} from "@multica/ui/icons";
import { toast } from "sonner";
import { copyText } from "@multica/ui/lib/clipboard";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  docDetailOptions,
  docTreeOptions,
  docCommentsOptions,
  folderAncestry,
  isDocNotFound,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Doc } from "@multica/core/types";
import { ContentEditor, useEditorUpload } from "../../editor";
import { ActorAvatar } from "../../common/actor-avatar";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { AppLink, useNavigation } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { useDocDraft } from "./use-doc-draft";
import { DocOutline, MIN_OUTLINE_HEADINGS, useDocOutline } from "./doc-outline";
import { DocCommentsPanel, groupDocThreads } from "./doc-comments-panel";
import { captureDocAnchor, type DocAnchor } from "./doc-comment-anchors";
import { useDocCommentHighlights } from "./use-doc-comment-highlights";
import { MoveDialog } from "./move-dialog";
import { DeleteDocDialog } from "./doc-tree";
import { useCanDeleteDoc } from "./use-can-delete-doc";
import { DocNotFound } from "./doc-not-found";
import { DocsGate } from "./docs-gate";

export function DocPage({ docId }: { docId: string }) {
  return (
    <DocsGate>
      <DocPageContent docId={docId} />
    </DocsGate>
  );
}

function DocPageContent({ docId }: { docId: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("docs");
  const query = useQuery(docDetailOptions(wsId, docId));
  // Also when it was open: a refetch answered 404 once someone deleted it.
  if (isDocNotFound(query.error)) return <DocNotFound docId={docId} />;
  if (!query.data)
    return (
      <DocPageSkeleton docId={docId}>
        {query.isError ? (
          <Button onClick={() => void query.refetch()}>
            {t(($) => $.editor.retry)}
          </Button>
        ) : null}
      </DocPageSkeleton>
    );
  return <DocEditor key={`${wsId}:${docId}`} doc={query.data} />;
}

/**
 * The editor's frame while the doc loads: the header and reading column sit
 * where the loaded page puts them, so nothing moves when the content lands.
 * The tree already knows the title and folder, so those show for real.
 */
function DocPageSkeleton({
  docId,
  children,
}: {
  docId: string;
  children?: React.ReactNode;
}) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data: tree } = useQuery(docTreeOptions(wsId));
  const summary = tree?.docs.find((d) => d.id === docId);
  const ancestors = folderAncestry(tree?.folders ?? [], summary?.folderId ?? null);
  return (
    <div className="flex h-full min-h-0 flex-col">
      <BreadcrumbHeader
        segments={[
          { href: p.docs(), label: t(($) => $.kb) },
          ...ancestors.map((f) => ({ href: p.docFolder(f.id), label: f.title })),
        ]}
        leaf={
          summary ? (
            <span className="truncate">{summary.title || t(($) => $.untitled)}</span>
          ) : (
            <Skeleton className="h-4 w-32" />
          )
        }
      />
      <div className="@container min-h-0 flex-1 overflow-auto">
        <div aria-busy className="mx-auto max-w-4xl px-6 py-8 @3xl:px-16">
          {children ?? (
            <>
              {summary ? (
                <div className="text-display-sm font-semibold">
                  {summary.title || (
                    <span className="text-muted-foreground">{t(($) => $.untitled)}</span>
                  )}
                </div>
              ) : (
                <Skeleton className="h-8 w-2/3" />
              )}
              <div className="mt-3 mb-8 flex items-center gap-2">
                <Skeleton className="size-4 rounded-full" />
                <Skeleton className="h-3.5 w-40" />
              </div>
              <div className="space-y-3">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-11/12" />
                <Skeleton className="h-4 w-4/5" />
                <Skeleton className="h-4 w-2/3" />
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
function DocEditor({ doc }: { doc: Doc }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data: tree } = useQuery(docTreeOptions(wsId));
  const { getActorName } = useActorName();
  const timeAgo = useTimeAgo();
  const { draft, status, dirty, change, retry, reload } = useDocDraft(doc);
  const [moving, setMoving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const canDelete = useCanDeleteDoc()(doc);
  const ancestors = folderAncestry(tree?.folders ?? [], doc.folderId);
  const author = getActorName(doc.updatedByType, doc.updatedById);
  const { getShareableUrl, searchParams } = useNavigation();
  const copyLink = () => {
    void copyText(getShareableUrl(p.docDetail(doc.id))).then((ok) => {
      if (ok) toast.success(t(($) => $.link_copied));
    });
  };
  const { uploadWithToast } = useEditorUpload();
  const uploadFile = useCallback((file: File) => uploadWithToast(file), [uploadWithToast]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const outline = useDocOutline(bodyRef, scrollRef);
  const showOutline = outline.headings.length >= MIN_OUTLINE_HEADINGS;

  // Discussions. A link from the inbox names one with ?thread=.
  const { data: comments } = useQuery(docCommentsOptions(wsId, doc.id));
  const threads = useMemo(() => groupDocThreads(comments ?? []), [comments]);
  const linkedThread = searchParams.get("thread");
  const [commentsOpen, setCommentsOpen] = useState(!!linkedThread);
  const [activeThread, setActiveThread] = useState<string | null>(linkedThread);
  const [newAnchor, setNewAnchor] = useState<DocAnchor | null>(null);
  useEffect(() => {
    if (!linkedThread) return;
    setCommentsOpen(true);
    setActiveThread(linkedThread);
  }, [linkedThread]);
  const openThreads = threads.filter((th) => !th.root.resolvedAt);
  const anchored = useMemo(
    () =>
      threads
        .filter((th) => !th.root.resolvedAt || th.root.id === activeThread)
        .map((th) => ({
          id: th.root.id,
          quote: th.root.anchorQuote,
          prefix: th.root.anchorPrefix,
          suffix: th.root.anchorSuffix,
        })),
    [threads, activeThread],
  );
  const highlights = useDocCommentHighlights(bodyRef, anchored, activeThread);
  const activateThread = (id: string) => {
    setActiveThread(id);
    setCommentsOpen(true);
    highlights.reveal(id);
  };
  const commentAction = useMemo(
    () => ({
      label: t(($) => $.comments.add),
      onSelect: () => {
        const root = bodyRef.current?.querySelector<HTMLElement>(".ProseMirror");
        const anchor = root ? captureDocAnchor(root, window.getSelection()) : null;
        if (!anchor) return false;
        setNewAnchor(anchor);
        setActiveThread(null);
        setCommentsOpen(true);
        return true;
      },
    }),
    [t],
  );
  // A click inside highlighted text opens its discussion.
  const onBodyClick = () => {
    const selection = window.getSelection();
    if (!selection?.isCollapsed || !selection.anchorNode) return;
    const id = highlights.threadAt(selection.anchorNode, selection.anchorOffset);
    if (id) {
      setActiveThread(id);
      setCommentsOpen(true);
    }
  };
  return (
    <div className="flex h-full min-h-0 flex-col">
      <BreadcrumbHeader
        segments={[
          { href: p.docs(), label: t(($) => $.kb) },
          ...ancestors.map((f) => ({
            href: p.docFolder(f.id),
            label: f.title,
          })),
        ]}
        leaf={
          <span className="truncate">{doc.title || t(($) => $.untitled)}</span>
        }
        actions={
          <>
            <span role="status" className="px-1 text-caption text-muted-foreground">
              {status === "error" ? (
                <Button size="xs" variant="ghost" onClick={() => void retry()}>
                  {t(($) => $.editor.save_failed)}
                </Button>
              ) : status === "saving" || (dirty && status === "saved") ? (
                t(($) => $.editor.saving)
              ) : status !== "conflict" ? (
                t(($) => $.editor.saved)
              ) : null}
            </span>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={t(($) => $.actions.copy_link)}
              title={t(($) => $.actions.copy_link)}
              onClick={copyLink}
            >
              <LinkIcon className="size-4" />
            </Button>
            <Button
              size="sm"
              variant="ghost"
              aria-pressed={commentsOpen}
              className={commentsOpen ? "bg-surface-selected" : undefined}
              onClick={() => setCommentsOpen((open) => !open)}
            >
              <MessageSquare className="size-4" />
              {t(($) => $.comments.title)}
              {openThreads.length > 0 && (
                <span className="text-caption text-muted-foreground">{openThreads.length}</span>
              )}
            </Button>
            <AppLink
              href={p.docHistory(doc.id)}
              className="flex items-center gap-1 rounded-md px-2 py-1 text-caption hover:bg-surface-hover"
            >
              <History className="size-4" />
              {t(($) => $.actions.history)}
            </AppLink>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={t(($) => $.actions.more)}
                  />
                }
              >
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => setMoving(true)}>
                  <FolderInput />
                  {t(($) => $.actions.move)}
                </DropdownMenuItem>
                {canDelete && (
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setDeleting(true)}
                  >
                    <Trash2 />
                    {t(($) => $.actions.delete)}
                  </DropdownMenuItem>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
      />
      {status === "conflict" && (
        <div
          role="alert"
          className="flex flex-wrap items-center gap-2 border-b bg-warning/10 px-6 py-2 text-body"
        >
          <span className="flex-1">
            {t(($) => $.editor.conflict, { name: author })}
          </span>
          <Button size="sm" onClick={() => void reload()}>
            {t(($) => $.editor.reload)}
          </Button>
        </div>
      )}
      <div className="flex min-h-0 flex-1">
      <div ref={scrollRef} className="@container min-h-0 min-w-0 flex-1 overflow-auto">
        <div className="flex min-h-full">
          <div className="min-w-0 flex-1">
            <div className="mx-auto max-w-4xl px-6 py-8 @3xl:px-16">
              {draft.backup && (
                <details className="mb-5 rounded-md border p-3 text-body">
                  <summary className="cursor-pointer">
                    {t(($) => $.editor.draft)}
                  </summary>
                  <textarea
                    aria-label={t(($) => $.editor.draft)}
                    readOnly
                    value={`${draft.backup.title}\n\n${draft.backup.content}`}
                    className="mt-2 min-h-40 w-full resize-y rounded-md bg-muted p-2 font-mono text-caption"
                  />
                </details>
              )}
              <input
                aria-label={t(($) => $.editor.title_label)}
                value={draft.title}
                // A doc just created from the tree is blank: start in its title.
                autoFocus={!doc.title && !doc.content}
                maxLength={300}
                placeholder={t(($) => $.untitled)}
                onChange={(e) => change({ title: e.target.value })}
                className="w-full bg-transparent text-display-sm font-semibold outline-none"
              />
              <div className="mt-3 mb-8 flex items-center gap-2 text-caption text-muted-foreground">
                <ActorAvatar
                  actorType={doc.updatedByType}
                  actorId={doc.updatedById}
                  size="xs"
                />
                <span className="truncate">{author}</span>
                <span aria-hidden>·</span>
                <Tooltip>
                  <TooltipTrigger
                    render={<time dateTime={doc.updatedAt} className="shrink-0" />}
                  >
                    {t(($) => $.edited, { time: timeAgo(doc.updatedAt) })}
                  </TooltipTrigger>
                  <TooltipContent>
                    {new Date(doc.updatedAt).toLocaleString()}
                  </TooltipContent>
                </Tooltip>
              </div>
              <div ref={bodyRef} onClick={onBodyClick}>
                <ContentEditor
                  selectionAction={commentAction}
                  value={draft.content}
                  onUpdate={(content) => change({ content })}
                  debounceMs={0}
                  flushPendingOnUnmount
                  placeholder={t(($) => $.editor.placeholder)}
                  enableSlashCommands
                  slashCommandMode="blocks"
                  showBlockHandles
                  onUploadFile={uploadFile}
                  className="min-h-80"
                />
              </div>
            </div>
          </div>
          {/* The discussions panel takes the right side; the outline yields. */}
          {showOutline && !commentsOpen && (
            <aside className="hidden w-56 shrink-0 py-8 pr-6 @5xl:block">
              <DocOutline
                headings={outline.headings}
                activeIndex={outline.activeIndex}
                onSelect={outline.scrollTo}
              />
            </aside>
          )}
        </div>
      </div>
      {commentsOpen && (
        <DocCommentsPanel
          docId={doc.id}
          threads={threads}
          missing={highlights.missing}
          activeId={activeThread}
          draft={newAnchor}
          onActivate={activateThread}
          onDeactivate={() => setActiveThread(null)}
          onDraftDone={(id) => {
            setNewAnchor(null);
            if (id) setActiveThread(id);
          }}
          onClose={() => {
            setCommentsOpen(false);
            setNewAnchor(null);
          }}
        />
      )}
      </div>
      <MoveDialog
        target={moving ? { kind: "doc", id: doc.id, title: doc.title } : null}
        onOpenChange={setMoving}
      />
      <DeleteDocDialog
        target={deleting ? { kind: "doc", item: doc } : null}
        onOpenChange={setDeleting}
      />
    </div>
  );
}
