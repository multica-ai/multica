"use client";

import { memo, useCallback, useRef, useState } from "react";
import { CheckCircle2, ChevronRight, Copy, Download, Eye, FileText, MoreHorizontal, Pencil, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Card } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
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
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from "@multica/ui/components/ui/collapsible";
import { ActorAvatar } from "../../common/actor-avatar";
import { ReactionBar } from "@multica/ui/components/common/reaction-bar";
import { QuickEmojiPicker } from "@multica/ui/components/common/quick-emoji-picker";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import { timeAgo } from "@multica/core/utils";
import { ContentEditor, type ContentEditorRef, copyMarkdown, ReadonlyContent, useFileDropZone, FileDropOverlay, useDownloadAttachment, useAttachmentPreview, isPreviewable } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { ReplyInput } from "./reply-input";
import type { TimelineEntry, Attachment } from "@multica/core/types";
import { useCommentCollapseStore, useCommentDraftStore } from "@multica/core/issues/stores";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface CommentCardProps {
  issueId: string;
  entry: TimelineEntry;
  /**
   * Flat list of every nested reply under this thread root, in render order.
   * Computed once in `issue-detail.tsx`'s `timelineView` and stabilized so
   * the array reference only changes when *this* thread's replies change —
   * an unrelated thread receiving a new reply must NOT bust this card's
   * memo. Passing the full Map here used to do exactly that.
   */
  replies: TimelineEntry[];
  currentUserId?: string;
  /**
   * True when the current user is a workspace owner/admin and can therefore
   * moderate comments authored by anyone — restoring the admin override that
   * the backend already grants at `comment.go:507-512`. Computed once in
   * `issue-detail.tsx` and threaded down so neither this component nor
   * `CommentRow` has to rerun the rule per row.
   */
  canModerate?: boolean;
  onReply: (parentId: string, content: string, attachmentIds?: string[]) => Promise<void>;
  onEdit: (commentId: string, content: string) => Promise<void>;
  onDelete: (commentId: string) => void;
  onToggleReaction: (commentId: string, emoji: string) => void;
  /** Toggle the resolved state on the thread root. Only invoked for root entries. */
  onResolveToggle?: (commentId: string, resolved: boolean) => void;
  /**
   * When non-null, the thread root is currently rendered as a resolved-but-
   * expanded card. Pass a "Collapse" affordance into the header so the user
   * can fold the thread back to the bar; the parent owns the session state.
   */
  onCollapseResolved?: () => void;
  /** ID of the comment to highlight (flash animation). */
  highlightedCommentId?: string | null;
}

// ---------------------------------------------------------------------------
// Shared delete confirmation dialog
// ---------------------------------------------------------------------------

function DeleteCommentDialog({
  open,
  onOpenChange,
  onConfirm,
  hasReplies,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  hasReplies?: boolean;
}) {
  const { t } = useT("issues");
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.comment.delete_title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {hasReplies
              ? t(($) => $.comment.delete_desc_with_replies)
              : t(($) => $.comment.delete_desc)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.comment.cancel_action)}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onConfirm}>
            {t(($) => $.comment.delete_action)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

// ---------------------------------------------------------------------------
// Standalone attachment list — renders attachments not already in the markdown
// ---------------------------------------------------------------------------

function AttachmentList({ attachments, content, className }: { attachments?: Attachment[]; content?: string; className?: string }) {
  const { t } = useT("editor");
  const download = useDownloadAttachment();
  const preview = useAttachmentPreview();
  if (!attachments?.length) return null;
  // Skip attachments whose URL is already referenced in the markdown content,
  // and duplicates of the same file (same name/type/size) that are referenced.
  const standalone = content
    ? attachments.filter((a) => {
        if (content.includes(a.url)) return false;
        // Dedup: if another attachment with the same file identity is already
        // inline in the content, this is a duplicate upload — skip it.
        const hasSiblingInContent = attachments.some(
          (other) =>
            other.id !== a.id &&
            other.filename === a.filename &&
            other.content_type === a.content_type &&
            other.size_bytes === a.size_bytes &&
            content.includes(other.url),
        );
        if (hasSiblingInContent) return false;
        return true;
      })
    : attachments;
  if (!standalone.length) return null;

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      {standalone.map((a) => (
        <div
          key={a.id}
          className="flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted"
        >
          <FileText className="size-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm">{a.filename}</p>
          </div>
          {isPreviewable(a.content_type, a.filename) && (
            <button
              type="button"
              className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title={t(($) => $.attachment.preview)}
              aria-label={t(($) => $.attachment.preview)}
              onClick={() => preview.tryOpen(a)}
            >
              <Eye className="size-3.5" />
            </button>
          )}
          <button
            type="button"
            className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title={t(($) => $.image.download)}
            aria-label={t(($) => $.image.download)}
            onClick={() => download(a.id)}
          >
            <Download className="size-3.5" />
          </button>
        </div>
      ))}
      {preview.modal}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Single comment row (used for both parent and replies within the same Card)
// ---------------------------------------------------------------------------

function CommentRow({
  issueId,
  entry,
  currentUserId,
  canModerate = false,
  onEdit,
  onDelete,
  onToggleReaction,
}: {
  issueId: string;
  entry: TimelineEntry;
  currentUserId?: string;
  canModerate?: boolean;
  onEdit: (commentId: string, content: string) => Promise<void>;
  onDelete: (commentId: string) => void;
  onToggleReaction: (commentId: string, emoji: string) => void;
}) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const [editing, setEditing] = useState(false);
  const editEditorRef = useRef<ContentEditorRef>(null);
  const cancelledRef = useRef(false);
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editEditorRef.current?.uploadFile(f)),
    enabled: editing,
  });

  // Edit-mode draft: virtualization unmounts the card when it scrolls out
  // of viewport, taking the in-progress edit with it. Persist via store
  // so a scroll-away + scroll-back round-trip restores the user's edits.
  // Key includes issueId so two issues with the same comment id (impossible
  // but defensive) don't collide; cleared on cancel and on save.
  const editDraftKey = `edit:${issueId}:${entry.id}` as const;
  const getEditDraft = useCommentDraftStore.getState().getDraft;
  const setEditDraft = useCommentDraftStore((s) => s.setDraft);
  const clearEditDraft = useCommentDraftStore((s) => s.clearDraft);
  // Read the snapshot once when the edit pass mounts; ContentEditor only
  // honors `defaultValue` on mount, so a live store subscription here would
  // cause an extra unmount/remount on every keystroke.
  const editInitialValue = editing
    ? (getEditDraft(editDraftKey) ?? entry.content ?? "")
    : (entry.content ?? "");

  const isOwn = entry.actor_type === "member" && entry.actor_id === currentUserId;
  const canEditEntry = isOwn || (canModerate && entry.actor_type === "member");
  const canDeleteEntry = isOwn || canModerate;
  const isTemp = entry.id.startsWith("temp-");
  const [confirmDelete, setConfirmDelete] = useState(false);

  const startEdit = () => {
    cancelledRef.current = false;
    setEditing(true);
  };

  const cancelEdit = () => {
    cancelledRef.current = true;
    setEditing(false);
    clearEditDraft(editDraftKey);
  };

  const saveEdit = async () => {
    if (cancelledRef.current) return;
    const trimmed = editEditorRef.current
      ?.getMarkdown()
      ?.replace(/(\n\s*)+$/, "")
      .trim();
    if (!trimmed || trimmed === (entry.content ?? "").trim()) {
      setEditing(false);
      clearEditDraft(editDraftKey);
      return;
    }
    try {
      await onEdit(entry.id, trimmed);
      setEditing(false);
      clearEditDraft(editDraftKey);
    } catch {
      toast.error(t(($) => $.comment.update_failed));
    }
  };

  const reactions = entry.reactions ?? [];
  const contentText = entry.content ?? "";
  const isLongContent = contentText.length > 500 || contentText.split("\n").length > 8;

  return (
    <div className={`py-3${isTemp ? " opacity-60" : ""}`}>
      <div className="flex items-center gap-2.5">
        <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size={24} enableHoverCard showStatusDot />
        <span className="cursor-pointer text-sm font-medium">
          {getActorName(entry.actor_type, entry.actor_id)}
        </span>
        <Tooltip>
          <TooltipTrigger
            render={
              <span className="text-xs text-muted-foreground cursor-default">
                {timeAgo(entry.created_at)}
              </span>
            }
          />
          <TooltipContent side="top">
            {new Date(entry.created_at).toLocaleString()}
          </TooltipContent>
        </Tooltip>

        {!isTemp && (
          <div className="ml-auto flex items-center gap-0.5">
            <QuickEmojiPicker
              onSelect={(emoji) => onToggleReaction(entry.id, emoji)}
              align="end"
            />
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-sm" className="text-muted-foreground">
                  <MoreHorizontal className="h-4 w-4" />
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => {
                copyMarkdown(entry.content ?? "");
                toast.success(t(($) => $.comment.copied_toast));
              }}>
                <Copy className="h-3.5 w-3.5" />
                {t(($) => $.comment.copy_action)}
              </DropdownMenuItem>
              {(canEditEntry || canDeleteEntry) && (
                <>
                  <DropdownMenuSeparator />
                  {canEditEntry && (
                    <DropdownMenuItem onClick={startEdit}>
                      <Pencil className="h-3.5 w-3.5" />
                      {t(($) => $.comment.edit_action)}
                    </DropdownMenuItem>
                  )}
                  {canEditEntry && canDeleteEntry && <DropdownMenuSeparator />}
                  {canDeleteEntry && (
                    <DropdownMenuItem onClick={() => setConfirmDelete(true)} variant="destructive">
                      <Trash2 className="h-3.5 w-3.5" />
                      {t(($) => $.comment.delete_action)}
                    </DropdownMenuItem>
                  )}
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          <DeleteCommentDialog
            open={confirmDelete}
            onOpenChange={setConfirmDelete}
            onConfirm={() => onDelete(entry.id)}
          />
          </div>
        )}
      </div>

      {editing ? (
        <div
          {...dropZoneProps}
          className="relative mt-1.5 pl-8"
          onKeyDown={(e) => { if (e.key === "Escape") cancelEdit(); }}
        >
          <div className="text-sm leading-relaxed">
            <ContentEditor
              ref={editEditorRef}
              defaultValue={editInitialValue}
              placeholder={t(($) => $.comment.edit_placeholder)}
              onUpdate={(md) => {
                if (md.trim().length > 0) setEditDraft(editDraftKey, md);
                else clearEditDraft(editDraftKey);
              }}
              onSubmit={saveEdit}
              onUploadFile={(file) => uploadWithToast(file, { issueId })}
              debounceMs={100}
              currentIssueId={issueId}
              attachments={entry.attachments}
            />
          </div>
          <div className="flex items-center justify-between mt-2">
            <FileUploadButton
              size="sm"
              onSelect={(file) => editEditorRef.current?.uploadFile(file)}
            />
            <div className="flex items-center gap-2">
              <Button size="sm" variant="ghost" onClick={cancelEdit}>{t(($) => $.comment.cancel_edit)}</Button>
              <Button size="sm" variant="outline" onClick={saveEdit}>{t(($) => $.comment.save_action)}</Button>
            </div>
          </div>
          {isDragOver && <FileDropOverlay />}
        </div>
      ) : (
        <>
          <div className="mt-1.5 pl-8 text-sm leading-relaxed text-foreground/85">
            <ReadonlyContent content={entry.content ?? ""} attachments={entry.attachments} />
          </div>
          <AttachmentList attachments={entry.attachments} content={entry.content} className="mt-1.5 pl-8" />
          {!isTemp && (
            <ReactionBar
              reactions={reactions}
              currentUserId={currentUserId}
              onToggle={(emoji) => onToggleReaction(entry.id, emoji)}
              getActorName={getActorName}
              hideAddButton={!isLongContent}
              className="mt-1.5 pl-8"
            />
          )}
        </>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// CommentCard — One Card per thread (parent + all replies flat inside)
// ---------------------------------------------------------------------------

function CommentCardImpl({
  issueId,
  entry,
  replies,
  currentUserId,
  canModerate = false,
  onReply,
  onEdit,
  onDelete,
  onToggleReaction,
  onResolveToggle,
  onCollapseResolved,
  highlightedCommentId,
}: CommentCardProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const { uploadWithToast } = useFileUpload(api);
  const isCollapsed = useCommentCollapseStore((s) => s.isCollapsed(issueId, entry.id));
  const toggleCollapse = useCommentCollapseStore((s) => s.toggle);
  const open = !isCollapsed;
  const handleOpenChange = useCallback((_open: boolean) => toggleCollapse(issueId, entry.id), [toggleCollapse, issueId, entry.id]);
  const [editing, setEditing] = useState(false);
  const editEditorRef = useRef<ContentEditorRef>(null);
  const cancelledRef = useRef(false);
  const { isDragOver: parentDragOver, dropZoneProps: parentDropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editEditorRef.current?.uploadFile(f)),
    enabled: editing,
  });

  // Edit-mode draft (root comment). Same rationale as CommentRow's draft.
  const parentEditDraftKey = `edit:${issueId}:${entry.id}` as const;
  const getParentEditDraft = useCommentDraftStore.getState().getDraft;
  const setParentEditDraft = useCommentDraftStore((s) => s.setDraft);
  const clearParentEditDraft = useCommentDraftStore((s) => s.clearDraft);
  const parentEditInitialValue = editing
    ? (getParentEditDraft(parentEditDraftKey) ?? entry.content ?? "")
    : (entry.content ?? "");

  const isOwn = entry.actor_type === "member" && entry.actor_id === currentUserId;
  // Author-only edit is the same as before; admins additionally get edit
  // *and* delete on member-authored comments, plus delete on agent-authored
  // ones. Edit on agent comments is intentionally never offered — agents
  // own their own outputs.
  const canEditEntry = isOwn || (canModerate && entry.actor_type === "member");
  const canDeleteEntry = isOwn || canModerate;
  const isTemp = entry.id.startsWith("temp-");
  const [confirmDelete, setConfirmDelete] = useState(false);

  const startEdit = () => {
    cancelledRef.current = false;
    setEditing(true);
  };

  const cancelEdit = () => {
    cancelledRef.current = true;
    setEditing(false);
    clearParentEditDraft(parentEditDraftKey);
  };

  const saveEdit = async () => {
    if (cancelledRef.current) return;
    const trimmed = editEditorRef.current
      ?.getMarkdown()
      ?.replace(/(\n\s*)+$/, "")
      .trim();
    if (!trimmed || trimmed === (entry.content ?? "").trim()) {
      setEditing(false);
      clearParentEditDraft(parentEditDraftKey);
      return;
    }
    try {
      await onEdit(entry.id, trimmed);
      setEditing(false);
      clearParentEditDraft(parentEditDraftKey);
    } catch {
      toast.error(t(($) => $.comment.update_failed));
    }
  };

  // The parent precomputes the flat thread (using collectThreadReplies),
  // memoizes by thread, and stabilizes the array reference, so we render
  // straight from `replies` instead of re-walking the graph on every render.
  const allNestedReplies = replies;

  const replyCount = allNestedReplies.length;
  const contentPreview = (entry.content ?? "").replace(/\n/g, " ").slice(0, 80);
  const reactions = entry.reactions ?? [];
  const contentText = entry.content ?? "";
  const isLongContent = contentText.length > 500 || contentText.split("\n").length > 8;

  const isHighlighted = highlightedCommentId === entry.id;

  return (
    <Card className={cn("!py-0 !gap-0 overflow-hidden transition-colors duration-700", isTemp && "opacity-60", isHighlighted && "ring-2 ring-brand/50 bg-brand/5")}>
      {onCollapseResolved && (
        <button
          type="button"
          onClick={onCollapseResolved}
          className="flex w-full items-center justify-between border-b border-border/50 px-4 py-2.5 text-left text-sm text-muted-foreground hover:bg-muted/50 transition-colors"
          aria-label={t(($) => $.comment.resolve.collapse)}
        >
          <span className="flex items-center gap-2">
            <CheckCircle2 className="h-3.5 w-3.5" />
            {t(($) => $.comment.resolve.collapse)}
          </span>
          <ChevronRight className="h-3.5 w-3.5 -rotate-90" />
        </button>
      )}
      <Collapsible open={open} onOpenChange={handleOpenChange}>
        {/* Header — always visible, acts as toggle */}
        <div className="px-4 py-3">
          <div className="flex items-center gap-2.5">
            <CollapsibleTrigger className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors">
              <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-90")} />
            </CollapsibleTrigger>
            <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size={24} enableHoverCard showStatusDot />
            <span className="shrink-0 cursor-pointer text-sm font-medium">
              {getActorName(entry.actor_type, entry.actor_id)}
            </span>
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="shrink-0 text-xs text-muted-foreground cursor-default">
                    {timeAgo(entry.created_at)}
                  </span>
                }
              />
              <TooltipContent side="top">
                {new Date(entry.created_at).toLocaleString()}
              </TooltipContent>
            </Tooltip>

            {!open && contentPreview && (
              <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                {contentPreview}
              </span>
            )}
            {!open && replyCount > 0 && (
              <span className="shrink-0 text-xs text-muted-foreground">
                {t(($) => $.comment.reply_count, { count: replyCount })}
              </span>
            )}

            {open && !isTemp && (
              <div className="ml-auto flex items-center gap-0.5">
                <QuickEmojiPicker
                  onSelect={(emoji) => onToggleReaction(entry.id, emoji)}
                  align="end"
                />
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button variant="ghost" size="icon-sm" className="text-muted-foreground">
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => {
                    copyMarkdown(entry.content ?? "");
                    toast.success(t(($) => $.comment.copied_toast));
                  }}>
                    <Copy className="h-3.5 w-3.5" />
                    {t(($) => $.comment.copy_action)}
                  </DropdownMenuItem>
                  {onResolveToggle && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onClick={() => onResolveToggle(entry.id, !entry.resolved_at)}>
                        {entry.resolved_at ? (
                          <>
                            <RotateCcw className="h-3.5 w-3.5" />
                            {t(($) => $.comment.resolve.unresolve_action)}
                          </>
                        ) : (
                          <>
                            <CheckCircle2 className="h-3.5 w-3.5" />
                            {t(($) => $.comment.resolve.resolve_action)}
                          </>
                        )}
                      </DropdownMenuItem>
                    </>
                  )}
                  {(canEditEntry || canDeleteEntry) && (
                    <>
                      <DropdownMenuSeparator />
                      {canEditEntry && (
                        <DropdownMenuItem onClick={startEdit}>
                          <Pencil className="h-3.5 w-3.5" />
                          {t(($) => $.comment.edit_action)}
                        </DropdownMenuItem>
                      )}
                      {canEditEntry && canDeleteEntry && <DropdownMenuSeparator />}
                      {canDeleteEntry && (
                        <DropdownMenuItem onClick={() => setConfirmDelete(true)} variant="destructive">
                          <Trash2 className="h-3.5 w-3.5" />
                          {t(($) => $.comment.delete_action)}
                        </DropdownMenuItem>
                      )}
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
              <DeleteCommentDialog
                open={confirmDelete}
                onOpenChange={setConfirmDelete}
                onConfirm={() => onDelete(entry.id)}
                hasReplies
              />
              </div>
            )}
          </div>
        </div>

        {/* Collapsible body */}
        <CollapsibleContent>
          {/* Parent comment body */}
          <div className="px-4 pb-3">
            {editing ? (
              <div
                {...parentDropZoneProps}
                className="relative pl-10"
                onKeyDown={(e) => { if (e.key === "Escape") cancelEdit(); }}
              >
                <div className="text-sm leading-relaxed">
                  <ContentEditor
                    ref={editEditorRef}
                    defaultValue={parentEditInitialValue}
                    placeholder={t(($) => $.comment.edit_placeholder)}
                    onUpdate={(md) => {
                      if (md.trim().length > 0) setParentEditDraft(parentEditDraftKey, md);
                      else clearParentEditDraft(parentEditDraftKey);
                    }}
                    onSubmit={saveEdit}
                    onUploadFile={(file) => uploadWithToast(file, { issueId })}
                    debounceMs={100}
                    currentIssueId={issueId}
                    attachments={entry.attachments}
                  />
                </div>
                <div className="flex items-center justify-between mt-2">
                  <FileUploadButton
                    size="sm"
                    onSelect={(file) => editEditorRef.current?.uploadFile(file)}
                  />
                  <div className="flex items-center gap-2">
                    <Button size="sm" variant="ghost" onClick={cancelEdit}>{t(($) => $.comment.cancel_edit)}</Button>
                    <Button size="sm" variant="outline" onClick={saveEdit}>{t(($) => $.comment.save_action)}</Button>
                  </div>
                </div>
                {parentDragOver && <FileDropOverlay />}
              </div>
            ) : (
              <>
                <div className="pl-10 text-sm leading-relaxed text-foreground/85">
                  <ReadonlyContent content={entry.content ?? ""} attachments={entry.attachments} />
                </div>
                <AttachmentList attachments={entry.attachments} content={entry.content} className="mt-1.5 pl-10" />
                {!isTemp && (
                  <ReactionBar
                    reactions={reactions}
                    currentUserId={currentUserId}
                    onToggle={(emoji) => onToggleReaction(entry.id, emoji)}
                    getActorName={getActorName}
                    hideAddButton={!isLongContent}
                    className="mt-1.5 pl-10"
                  />
                )}
              </>
            )}
          </div>

          {/* Replies */}
          {allNestedReplies.map((reply) => (
            <div key={reply.id} id={`comment-${reply.id}`} className={cn("border-t border-border/50 px-4 transition-colors duration-700", highlightedCommentId === reply.id && "bg-brand/5")}>
              <CommentRow
                issueId={issueId}
                entry={reply}
                currentUserId={currentUserId}
                canModerate={canModerate}
                onEdit={onEdit}
                onDelete={onDelete}
                onToggleReaction={onToggleReaction}
              />
            </div>
          ))}

          {/* Reply input */}
          <div className="border-t border-border/50 px-4 py-2.5">
            <ReplyInput
              issueId={issueId}
              placeholder={t(($) => $.reply.placeholder)}
              size="sm"
              avatarType="member"
              avatarId={currentUserId ?? ""}
              draftKey={`reply:${issueId}:${entry.id}`}
              onSubmit={(content, attachmentIds) => onReply(entry.id, content, attachmentIds)}
            />
          </div>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  );
}

// Memoized so a long timeline (e.g. Inbox-embedded IssueDetail with thousands
// of comments) does not re-render every card on each parent state update or
// WS-driven cache refresh. Default shallow comparison is sufficient: the
// timeline grouping is useMemo'd in issue-detail.tsx (stable Map ref), and
// every callback is stabilized via useCallback in use-issue-timeline.ts.
const CommentCard = memo(CommentCardImpl);

export { CommentCard, type CommentCardProps };
