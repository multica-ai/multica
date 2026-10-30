"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertCircle,
  AlertTriangle,
  Check,
  Code,
  Eye,
  FolderInput,
  GitMerge,
  History,
  Link2,
  Loader2,
  MoreHorizontal,
  PanelRight,
  Trash2,
  X,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { docDetailOptions, docTreeOptions, folderAncestry, useMarkDocRead } from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Doc } from "@multica/core/types";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { AppLink, currentPath, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { DocSidePanel } from "./doc-side-panel";
import { DeleteDocDialog } from "./doc-tree";
import { HtmlDoc, type HtmlDocHandle, type HtmlDocMode } from "./html-doc";
import { MoveDialog, type MoveTarget } from "./move-dialog";
import { useDocDraft, type DraftIO, type DraftNotice, type DraftStatus } from "./use-doc-draft";

function SaveStatus({ status, onRetry }: { status: DraftStatus; onRetry: () => void }) {
  const { t } = useT("docs");
  if (status === "saving") {
    return (
      <span className="flex items-center gap-1 text-caption text-muted-foreground" aria-live="polite">
        <Loader2 className="size-3 animate-spin" />
        {t(($) => $.editor.saving)}
      </span>
    );
  }
  if (status === "error") {
    return (
      <button type="button" onClick={onRetry} className="flex items-center gap-1 text-caption text-destructive hover:underline">
        <AlertCircle className="size-3" />
        {t(($) => $.editor.save_failed)}
      </button>
    );
  }
  return (
    <span className="flex items-center gap-1 text-caption text-faint-foreground" aria-live="polite">
      <Check className="size-3" />
      {t(($) => $.editor.saved)}
    </span>
  );
}

function NoticeBar({
  notice,
  historyHref,
  onAcceptTheirs,
  onDismiss,
}: {
  notice: DraftNotice;
  historyHref: string;
  onAcceptTheirs: () => void;
  onDismiss: () => void;
}) {
  const { t } = useT("docs");
  const { getActorName } = useActorName();
  const name = getActorName(notice.who.type, notice.who.id);
  const conflict = notice.kind === "conflict";
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        "flex shrink-0 items-center gap-2 border-b px-4 py-1.5 text-caption",
        conflict ? "border-warning/30 bg-warning/10" : "bg-surface-hover/60",
      )}
    >
      {conflict ? (
        <AlertTriangle className="size-3.5 shrink-0 text-warning" />
      ) : (
        <GitMerge className="size-3.5 shrink-0 text-muted-foreground" />
      )}
      <span className="min-w-0 flex-1 truncate text-foreground">
        {notice.kind === "updated"
          ? t(($) => $.editor.notice_updated, { name })
          : notice.kind === "merged"
            ? t(($) => $.editor.notice_merged, { name })
            : t(($) => $.editor.notice_conflict, { name })}
      </span>
      {conflict && (
        <Button size="xs" variant="outline" onClick={onAcceptTheirs}>
          {t(($) => $.editor.use_theirs, { name })}
        </Button>
      )}
      <AppLink href={historyHref} className="shrink-0 text-muted-foreground hover:text-foreground hover:underline">
        {t(($) => $.editor.view_changes)}
      </AppLink>
      <button
        type="button"
        aria-label={t(($) => $.actions.dismiss)}
        onClick={onDismiss}
        className="flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground"
      >
        <X className="size-3.5" />
      </button>
    </div>
  );
}

function DocEditor({ doc, actions, banner }: { doc: Doc; actions: React.ReactNode; banner: React.ReactNode }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const editorRef = useRef<ContentEditorRef>(null);
  const htmlRef = useRef<HtmlDocHandle>(null);
  const [htmlMode, setHtmlMode] = useState<HtmlDocMode>("preview");
  const isHtml = doc.format === "html";

  const io = useMemo<DraftIO>(
    () =>
      isHtml
        ? {
            read: () => htmlRef.current?.read() ?? doc.content,
            adopt: (text) => htmlRef.current?.adopt(text),
          }
        : {
            read: () => {
              const editor = editorRef.current;
              if (!editor) return doc.content;
              editor.flushPendingUpdate();
              return editor.getMarkdown().trim();
            },
            adopt: (text) => editorRef.current?.adoptContent(text),
          },
    // The doc's first content is only a fallback before the editor mounts.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [isHtml],
  );
  const draft = useDocDraft(doc, io);

  return (
    <>
      <DocHeader
        doc={doc}
        title={draft.title}
        status={<SaveStatus status={draft.status} onRetry={draft.retry} />}
        actions={actions}
        htmlMode={isHtml ? htmlMode : null}
        onHtmlModeChange={setHtmlMode}
      />
      {banner}
      {draft.notice && (
        <NoticeBar
          notice={draft.notice}
          historyHref={p.docHistory(doc.id)}
          onAcceptTheirs={draft.acceptTheirs}
          onDismiss={draft.dismissNotice}
        />
      )}
      {isHtml ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="shrink-0 border-b px-6 py-2">
            <TitleInput value={draft.title} onChange={draft.setTitle} className="text-title-sm" />
          </div>
          <HtmlDoc
            ref={htmlRef}
            initial={doc.content}
            title={draft.title}
            mode={htmlMode}
            onChange={draft.onContentChange}
          />
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="prose-document mx-auto w-full max-w-3xl px-6 pt-12 pb-24 sm:px-10">
            <TitleInput
              value={draft.title}
              onChange={draft.setTitle}
              onEnter={() => editorRef.current?.focus()}
              className="prose-document-title mb-6"
            />
            <ContentEditor
              ref={editorRef}
              defaultValue={doc.content}
              placeholder={t(($) => $.editor.placeholder)}
              onUpdate={(markdown) => draft.onContentChange(markdown)}
              onReady={draft.captureBase}
              debounceMs={1500}
              flushPendingOnUnmount
              className="min-h-[50vh]"
            />
          </div>
        </div>
      )}
    </>
  );
}

/** Wraps instead of scrolling sideways; a title is one line, so Enter moves on. */
function TitleInput({
  value,
  onChange,
  onEnter,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  onEnter?: () => void;
  className?: string;
}) {
  const { t } = useT("docs");
  const ref = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [value]);
  return (
    <textarea
      ref={ref}
      rows={1}
      value={value}
      maxLength={300}
      aria-label={t(($) => $.editor.title_label)}
      placeholder={t(($) => $.untitled)}
      onChange={(event) => onChange(event.target.value.replace(/\n/g, " "))}
      onKeyDown={(event) => {
        if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
        event.preventDefault();
        onEnter?.();
      }}
      className={cn(
        "block w-full resize-none overflow-hidden bg-transparent font-semibold text-foreground outline-none placeholder:text-faint-foreground",
        className,
      )}
    />
  );
}

function DocHeader({
  doc,
  title,
  status,
  actions,
  htmlMode,
  onHtmlModeChange,
}: {
  doc: Doc;
  title: string;
  status: React.ReactNode;
  actions: React.ReactNode;
  htmlMode: HtmlDocMode | null;
  onHtmlModeChange: (mode: HtmlDocMode) => void;
}) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data: tree } = useQuery(docTreeOptions(wsId));
  const folders = tree ? folderAncestry(tree.folders, doc.folder_id) : [];

  return (
    <BreadcrumbHeader
      segments={[
        { href: p.docs(), label: t(($) => $.kb) },
        ...folders.map((folder) => ({
          href: p.docFolder(folder.id),
          label: folder.title,
          className: "min-w-0 max-w-40 truncate",
        })),
      ]}
      leaf={<span className="min-w-0 truncate text-foreground">{title || t(($) => $.untitled)}</span>}
      actions={
        <>
          {status}
          {htmlMode && (
            <div
              role="group"
              aria-label={t(($) => $.html.mode_label)}
              className="ml-2 flex items-center gap-0.5 rounded-md bg-muted p-0.5"
            >
              {(["preview", "source"] as const).map((mode) => (
                <button
                  key={mode}
                  type="button"
                  aria-pressed={htmlMode === mode}
                  onClick={() => onHtmlModeChange(mode)}
                  className={cn(
                    "flex h-6 items-center gap-1 rounded-xs px-2 text-caption font-medium transition-colors",
                    htmlMode === mode ? "bg-surface text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {mode === "preview" ? <Eye className="size-3" /> : <Code className="size-3" />}
                  {mode === "preview" ? t(($) => $.html.preview) : t(($) => $.html.source)}
                </button>
              ))}
            </div>
          )}
          {actions}
        </>
      }
    />
  );
}

function UnreadBanner({ doc, since, onDismiss }: { doc: Doc; since: string | null; onDismiss: () => void }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const { getActorName } = useActorName();
  const href = since ? `${p.docHistory(doc.id)}?since=${encodeURIComponent(since)}` : p.docHistory(doc.id);
  return (
    <div className="flex shrink-0 items-center gap-2 border-b bg-brand/5 px-4 py-1.5 text-caption">
      <span className="size-1.5 shrink-0 rounded-full bg-brand" />
      <span className="min-w-0 flex-1 truncate">
        {t(($) => $.editor.unread_banner, { name: getActorName(doc.updated_by_type, doc.updated_by_id) })}
      </span>
      <AppLink href={href} className="shrink-0 font-medium text-brand hover:underline">
        {t(($) => $.editor.view_changes)}
      </AppLink>
      <button
        type="button"
        aria-label={t(($) => $.actions.dismiss)}
        onClick={onDismiss}
        className="flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground"
      >
        <X className="size-3.5" />
      </button>
    </div>
  );
}

function RefreshFailedBanner({ onRetry }: { onRetry: () => void }) {
  const { t } = useT("docs");
  return (
    <div role="alert" className="flex shrink-0 items-center gap-2 border-b border-warning/30 bg-warning/10 px-4 py-1.5 text-caption">
      <AlertTriangle className="size-3.5 shrink-0 text-warning" />
      <span className="min-w-0 flex-1 truncate text-foreground">{t(($) => $.editor.refresh_failed)}</span>
      <Button size="xs" variant="outline" onClick={onRetry}>
        {t(($) => $.editor.retry)}
      </Button>
    </div>
  );
}

export function DocDetailPage({ docId }: { docId: string }) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const navigation = useNavigation();
  const { data: doc, isPending, error, refetch } = useQuery(docDetailOptions(wsId, docId));
  const markRead = useMarkDocRead();
  const [panelOpen, setPanelOpen] = useState(true);
  const [moveTarget, setMoveTarget] = useState<MoveTarget | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [unread, setUnread] = useState<{ since: string | null } | null>(null);

  // Opening a doc reads it. The banner remembers what was new on arrival.
  const openedRef = useRef<string | null>(null);
  useEffect(() => {
    if (!doc || openedRef.current === doc.id) return;
    openedRef.current = doc.id;
    setUnread(doc.unread ? { since: doc.last_seen_at } : null);
    markRead.mutate(doc.id);
  }, [doc, markRead]);

  if (isPending) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-4 w-40" />
        </div>
        <div className="mx-auto w-full max-w-3xl space-y-3 px-6 pt-12 sm:px-10">
          <Skeleton className="mb-6 h-10 w-2/3" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
        </div>
      </div>
    );
  }

  // Only a doc that never loaded gets the error page. Once it has, a failed
  // background refresh must leave the editor — and its unsaved draft — alone.
  if (!doc) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-center">
        <AlertCircle className="size-8 text-faint-foreground" />
        <p className="text-body font-medium">{t(($) => $.not_found.title)}</p>
        <AppLink href={p.docs()} className="text-caption text-brand hover:underline">
          {t(($) => $.not_found.back)}
        </AppLink>
      </div>
    );
  }

  const actions = (
    <>
      <IconButton
        label={t(($) => $.actions.history)}
        tooltipSide="bottom"
        className="text-muted-foreground"
        onClick={() => navigation.push(p.docHistory(doc.id))}
      >
        <History />
      </IconButton>
      <IconButton
        label={t(($) => $.actions.toggle_panel)}
        tooltipSide="bottom"
        variant={panelOpen ? "secondary" : "ghost"}
        className={panelOpen ? "" : "text-muted-foreground"}
        onClick={() => setPanelOpen((open) => !open)}
      >
        <PanelRight />
      </IconButton>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={<IconButton label={t(($) => $.actions.more)} tooltipSide="bottom" className="text-muted-foreground" />}
        >
          <MoreHorizontal />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-auto min-w-44">
          <DropdownMenuItem
            onClick={() => {
              void copyText(navigation.getShareableUrl?.(currentPath(navigation)) ?? currentPath(navigation)).then(
                (ok) => ok && toast.success(t(($) => $.actions.link_copied)),
              );
            }}
          >
            <Link2 />
            {t(($) => $.actions.copy_link)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setMoveTarget({ kind: "doc", id: doc.id, title: doc.title })}>
            <FolderInput />
            {t(($) => $.actions.move)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => setDeleting(true)}>
            <Trash2 />
            {t(($) => $.actions.delete)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );

  return (
    <div className="flex min-h-0 flex-1">
      <div className="flex min-w-0 flex-1 flex-col">
        <DocEditor
          key={doc.id}
          doc={doc}
          actions={actions}
          banner={
            <>
              {error && <RefreshFailedBanner onRetry={() => void refetch()} />}
              {unread && <UnreadBanner doc={doc} since={unread.since} onDismiss={() => setUnread(null)} />}
            </>
          }
        />
      </div>
      {panelOpen && (
        <aside className="hidden w-80 shrink-0 border-l md:block">
          <DocSidePanel doc={doc} />
        </aside>
      )}
      <MoveDialog target={moveTarget} onOpenChange={(open) => !open && setMoveTarget(null)} />
      <DeleteDocDialog
        target={deleting ? { kind: "doc", doc } : null}
        onOpenChange={(open) => !open && setDeleting(false)}
      />
    </div>
  );
}
