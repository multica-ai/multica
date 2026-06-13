"use client";

// CEREBRO-PATCH(comment-card-cerebro): cerebro modification of upstream file

import { BellRing, CheckCircle2, ChevronRight, Copy, GitFork, MessageCircleQuestion, MessagesSquare, MoreHorizontal, Pencil, RotateCcw, Trash2 } from "lucide-react";
import { lazy, memo, Suspense, useCallback, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Card } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
// CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 select-mode checkboxes for moving comments to a new thread.
import { Checkbox } from "@multica/ui/components/ui/checkbox";
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
import { copyText } from "@multica/ui/lib/clipboard";
import { useActorName } from "@multica/core/workspace/hooks";
import { useTimeAgo } from "../../i18n";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { ReplyInput } from "./reply-input";
// CEREBRO-PATCH(comment-edit-attachment-binding): FIR-2394 — pure binding helpers extracted for robustness + unit coverage.
import { attachmentReferencedInContent, collectActiveAttachmentIds } from "./comment-attachment-binding";
import { AttachmentList } from "@multica/cerebro-attachments/views";
// CEREBRO-PATCH(comment-reminder-menu): FIR-2643 — "Remind me" on a comment lives in cerebro-inbox.
import { useCommentReminder } from "@multica/cerebro-inbox";
// CEREBRO-PATCH(issue-comment-cost-badge-import): FIR-39 per-comment cost badge.
import { CommentCostBadge } from "@multica/cerebro-channels";
import type { Attachment, TimelineEntry } from "@multica/core/types";
import { useCommentCollapseStore, useCommentDraftStore } from "@multica/core/issues/stores";
import { useT } from "../../i18n";

const LazyReadonlyContent = lazy(() =>
  import("../../editor/readonly-content").then((mod) => ({
    default: mod.ReadonlyContent,
  })),
);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Flatten a reply tree into a chronological list. Depth-first traversal alone
// places a sibling's deep descendants before later siblings, scrambling the
// visible order — sort by created_at after collection to fix that.
export function flattenReplies(
  rootId: string,
  repliesByParent: Map<string, TimelineEntry[]>,
): TimelineEntry[] {
  const flat: TimelineEntry[] = [];
  const visit = (parentId: string) => {
    const children = repliesByParent.get(parentId) ?? [];
    for (const child of children) {
      flat.push(child);
      visit(child.id);
    }
  };
  visit(rootId);
  flat.sort((a, b) => a.created_at.localeCompare(b.created_at));
  return flat;
}

// CEREBRO-PATCH(issue-ask-ui): JEH-1576 renders `multica issue ask` comments as user questions in the browser.
export function isUserQuestionComment(entry: Pick<TimelineEntry, "type" | "comment_type">): boolean {
  return entry.type === "comment" && entry.comment_type === "question";
}
// CEREBRO-PATCH(wakeup-activity-line): TECH-3038 Phase 1 — wakeup dispatch comments render as compact activity lines.
export function isWakeupComment(entry: Pick<TimelineEntry, "type" | "comment_type">): boolean { // CEREBRO-PATCH(wakeup-activity-line): TECH-3038 Phase 1
  return entry.type === "comment" && entry.comment_type === "wakeup";
}

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
  onEdit: (commentId: string, content: string, attachmentIds: string[]) => Promise<void>;
  onDelete: (commentId: string) => void;
  onToggleReaction: (commentId: string, emoji: string) => void;
  /** True when the root thread can be lifted into a child issue. */
  canMoveToSubIssue?: boolean;
  onMoveToSubIssue?: (commentId: string) => Promise<void>;
  /**
   * CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 — when set, the kebab
   * menu offers "Reply in new thread", which enters a select mode where the
   * user picks comments in this thread and lifts them into a new thread on the
   * same issue. `onMoveToNewThread` receives the picked ids in render order.
   */
  canMoveToNewThread?: boolean;
  onMoveToNewThread?: (commentIds: string[]) => Promise<void>;
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
  /**
   * CEREBRO-PATCH(reply-target-agent-indicator): FIR-2392 — agent the
   * backend trigger will wake when the reply has no @agent mentions
   * (issue assignee or, when the assignee is a squad, the squad leader).
   * Resolved once in `issue-detail.tsx` and forwarded so the reply field
   * indicator matches the trigger, not the replied-to comment author.
   */
  triggerAgentId?: string;
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

// CEREBRO-PATCH(comments-move-to-subissue-ui): JEH-1309 confirmation before lifting a thread to a sub-issue.
function MoveCommentToSubIssueDialog({
  open,
  onOpenChange,
  onConfirm,
  moving,
  replyCount,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  moving?: boolean;
  replyCount: number;
}) {
  const { t } = useT("issues");
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.comment.move.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.comment.move.description, { count: replyCount })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={moving}>{t(($) => $.comment.cancel_action)}</AlertDialogCancel>
          <AlertDialogAction
            disabled={moving}
            onClick={(event) => {
              event.preventDefault();
              onConfirm();
            }}
          >
            {moving ? t(($) => $.comment.move.moving_action) : t(($) => $.comment.move.action)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function sameIdSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((id) => set.has(id));
}

function initialStandaloneAttachmentIds(entry: TimelineEntry): Set<string> {
  const content = entry.content ?? "";
  return new Set(
    (entry.attachments ?? [])
      .filter((attachment) => !attachmentReferencedInContent(content, attachment))
      .map((attachment) => attachment.id),
  );
}

// ---------------------------------------------------------------------------
// Shared edit-attachment state hook
// ---------------------------------------------------------------------------

function useEditAttachmentState(
  issueId: string,
  entry: TimelineEntry,
  onEdit: (commentId: string, content: string, attachmentIds: string[]) => Promise<void>,
) {
  const { t } = useT("issues");
  const { uploadWithToast } = useFileUpload(api);
  const [editing, setEditing] = useState(false);
  const editorRef = useRef<ContentEditorRef>(null);
  const cancelledRef = useRef(false);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const [retainedStandaloneIds, setRetainedStandaloneIds] = useState<Set<string> | null>(null);

  const editorAttachments = pendingAttachments.length > 0
    ? [...(entry.attachments ?? []), ...pendingAttachments]
    : entry.attachments;

  const handleUpload = useCallback(async (file: File) => {
    const result = await uploadWithToast(file, { issueId });
    if (result) setPendingAttachments((prev) => [...prev, result]);
    return result;
  }, [uploadWithToast, issueId]);

  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
    enabled: editing,
  });

  const draftKey = `edit:${issueId}:${entry.id}` as const;
  const getDraft = useCommentDraftStore.getState().getDraft;
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const clearDraft = useCommentDraftStore((s) => s.clearDraft);

  const initialValue = editing
    ? (getDraft(draftKey) ?? entry.content ?? "")
    : (entry.content ?? "");

  const standaloneEditAttachments = (entry.attachments ?? []).filter((a) =>
    retainedStandaloneIds?.has(a.id),
  );

  const resetState = () => {
    setEditing(false);
    setPendingAttachments([]);
    setRetainedStandaloneIds(null);
    clearDraft(draftKey);
  };

  const startEdit = () => {
    cancelledRef.current = false;
    setRetainedStandaloneIds(initialStandaloneAttachmentIds(entry));
    setEditing(true);
  };

  const cancelEdit = () => {
    cancelledRef.current = true;
    resetState();
  };

  const saveEdit = async () => {
    if (cancelledRef.current) return;
    const trimmed = editorRef.current
      ?.getMarkdown()
      ?.replace(/(\n\s*)+$/, "")
      .trim();
    if (!trimmed) return;
    const activeIds = collectActiveAttachmentIds(
      trimmed,
      [...(entry.attachments ?? []), ...pendingAttachments],
      retainedStandaloneIds,
    );
    const attachmentsChanged = !sameIdSet(activeIds, (entry.attachments ?? []).map((a) => a.id));
    if (trimmed === (entry.content ?? "").trim() && !attachmentsChanged) {
      resetState();
      return;
    }
    try {
      await onEdit(entry.id, trimmed, activeIds);
      resetState();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.comment.update_failed),
      );
    }
  };

  return {
    editing,
    editorRef,
    editorAttachments,
    handleUpload,
    isDragOver,
    dropZoneProps,
    draftKey,
    setDraft,
    clearDraft,
    initialValue,
    standaloneEditAttachments,
    retainedStandaloneIds,
    setRetainedStandaloneIds,
    startEdit,
    cancelEdit,
    saveEdit,
  };
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
  // CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 select-mode + new-thread entry on replies.
  selectMode = false,
  selected = false,
  onToggleSelect,
  canStartNewThread = false,
  onStartNewThread,
}: {
  issueId: string;
  entry: TimelineEntry;
  currentUserId?: string;
  canModerate?: boolean;
  onEdit: (commentId: string, content: string, attachmentIds: string[]) => Promise<void>;
  onDelete: (commentId: string) => void;
  onToggleReaction: (commentId: string, emoji: string) => void;
  selectMode?: boolean;
  selected?: boolean;
  onToggleSelect?: (commentId: string) => void;
  canStartNewThread?: boolean;
  onStartNewThread?: (commentId: string) => void;
}) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const edit = useEditAttachmentState(issueId, entry, onEdit);
  const isOwn = entry.actor_type === "member" && entry.actor_id === currentUserId;
  const canEditEntry = isOwn || (canModerate && entry.actor_type === "member");
  const canDeleteEntry = isOwn || canModerate;
  // CEREBRO-PATCH(comment-temp-optimistic): dim + gate reactions on optimistic temp rows.
  const isTemp = entry.id.startsWith("temp-");
  const [confirmDelete, setConfirmDelete] = useState(false);
  // CEREBRO-PATCH(comment-reminder-menu): FIR-2643 — "Remind me" surface (cerebro-inbox).
  const reminder = useCommentReminder(issueId);

  const reactions = entry.reactions ?? [];
  const contentText = entry.content ?? "";
  const isLongContent = contentText.length > 500 || contentText.split("\n").length > 8;

  return (
    <div className={`py-3${isTemp ? " opacity-60" : ""}`}>
      <div className="flex items-center gap-2.5">
        {/* CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 reply select checkbox. */}
        {selectMode && (
          <Checkbox
            checked={selected}
            onCheckedChange={() => onToggleSelect?.(entry.id)}
            aria-label={t(($) => $.comment.move_thread.action)}
          />
        )}
        <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size={24} enableHoverCard showStatusDot />
        {/* CEREBRO-PATCH(comment-meta-below-name): TECH-3032 — name row, time+cost sub-row. */}
        <div className="flex flex-col gap-0.5">
          <span className="cursor-pointer text-sm font-medium leading-none">
            {getActorName(entry.actor_type, entry.actor_id)}
          </span>
          <div className="flex items-center gap-1.5">
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
            <CommentCostBadge issueId={issueId} commentId={entry.id} authorType={entry.actor_type} />
          </div>
        </div>

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
                void copyText(entry.content ?? "").then((ok) => {
                  if (ok) toast.success(t(($) => $.comment.copied_toast));
                });
              }}>
                <Copy className="h-3.5 w-3.5" />
                {t(($) => $.comment.copy_action)}
              </DropdownMenuItem>
              {/* CEREBRO-PATCH(comment-reminder-menu): FIR-2643 — remind me about this comment (all comments). */}
              {reminder.enabled && (
                <DropdownMenuItem onClick={() => reminder.openReminder(entry.id, entry.content ?? "")}>
                  <BellRing className="h-3.5 w-3.5" />
                  {reminder.label}
                </DropdownMenuItem>
              )}
              {/* CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 start a new thread from this reply. */}
              {canStartNewThread && (isOwn || canModerate) && onStartNewThread && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onClick={() => onStartNewThread(entry.id)}>
                    <MessagesSquare className="h-3.5 w-3.5" />
                    {t(($) => $.comment.move_thread.action)}
                  </DropdownMenuItem>
                </>
              )}
              {(canEditEntry || canDeleteEntry) && (
                <>
                  <DropdownMenuSeparator />
                  {canEditEntry && (
                    <DropdownMenuItem onClick={edit.startEdit}>
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
          {/* CEREBRO-PATCH(comment-reminder-menu): FIR-2643 — dialog lives outside the dropdown so it survives menu close. */}
          {reminder.dialog}
        </div>
      </div>

      {edit.editing ? (
        <div
          {...edit.dropZoneProps}
          className="relative mt-1.5 pl-2 sm:pl-8"
          onKeyDown={(e) => { if (e.key === "Escape") edit.cancelEdit(); }}
        >
          <div className="text-sm leading-relaxed">
            <ContentEditor
              ref={edit.editorRef}
              defaultValue={edit.initialValue}
              placeholder={t(($) => $.comment.edit_placeholder)}
              onUpdate={(md) => {
                if (md.trim().length > 0) edit.setDraft(edit.draftKey, md);
                else edit.clearDraft(edit.draftKey);
              }}
              onSubmit={edit.saveEdit}
              onUploadFile={edit.handleUpload}
              debounceMs={100}
              currentIssueId={issueId}
            />
          </div>
          <div className="flex items-center justify-between mt-2">
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              {edit.standaloneEditAttachments.length > 0 && (
                <AttachmentList
                  attachments={edit.standaloneEditAttachments}
                  className="max-w-full"
                  onRemove={(attachmentId) =>
                    edit.setRetainedStandaloneIds((ids) => {
                      const next = new Set(ids ?? []);
                      next.delete(attachmentId);
                      return next;
                    })
                  }
                />
              )}
              {/* CEREBRO-PATCH(file-upload-button-api): onAttach/onEmbed popup picker;
                  uploads route through handleEditUpload so they bind on save. */}
              <FileUploadButton
                size="sm"
                multiple
                onAttach={(files) => files.forEach((f) => edit.handleUpload(f))}
                onEmbed={(files) => files.forEach((f) => edit.editorRef.current?.uploadFile(f, { embedImage: true }))}
              />
            </div>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="ghost" onClick={edit.cancelEdit}>{t(($) => $.comment.cancel_edit)}</Button>
              <Button size="sm" variant="outline" onClick={edit.saveEdit}>{t(($) => $.comment.save_action)}</Button>
            </div>
          </div>
          {edit.isDragOver && <FileDropOverlay />}
        </div>
      ) : (
        <>
          <div className="mt-1.5 pl-2 sm:pl-8 text-sm leading-relaxed text-foreground/85">
            <Suspense fallback={<p className="whitespace-pre-wrap text-sm leading-relaxed">{entry.content ?? ""}</p>}>
              <LazyReadonlyContent content={entry.content ?? ""} attachments={entry.attachments} />
            </Suspense>
          </div>
          <AttachmentList attachments={entry.attachments} content={entry.content} className="mt-1.5 pl-2 sm:pl-8" />
          {!isTemp && (
            <ReactionBar
              reactions={reactions}
              currentUserId={currentUserId}
              onToggle={(emoji) => onToggleReaction(entry.id, emoji)}
              getActorName={getActorName}
              hideAddButton={!isLongContent}
              className="mt-1.5 pl-2 sm:pl-8"
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
  canMoveToSubIssue = false,
  onMoveToSubIssue,
  canMoveToNewThread = false,
  onMoveToNewThread,
  onResolveToggle,
  onCollapseResolved,
  highlightedCommentId,
  triggerAgentId,
}: CommentCardProps) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const isCollapsed = useCommentCollapseStore((s) => s.isCollapsed(issueId, entry.id));
  const toggleCollapse = useCommentCollapseStore((s) => s.toggle);
  // CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 select mode keeps the thread expanded so every comment is pickable.
  const [selectMode, setSelectMode] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set());
  const [movingThread, setMovingThread] = useState(false);
  const open = !isCollapsed || selectMode;
  const handleOpenChange = useCallback((_open: boolean) => toggleCollapse(issueId, entry.id), [toggleCollapse, issueId, entry.id]);
  const edit = useEditAttachmentState(issueId, entry, onEdit);
  const isOwn = entry.actor_type === "member" && entry.actor_id === currentUserId;
  const canEditEntry = isOwn || (canModerate && entry.actor_type === "member");
  const canDeleteEntry = isOwn || canModerate;
  // CEREBRO-PATCH(comments-move-to-subissue-ui): JEH-1309 lift-thread gating + optimistic temp dimming.
  const canMoveEntry = canMoveToSubIssue && !!onMoveToSubIssue && (isOwn || canModerate);
  const isTemp = entry.id.startsWith("temp-");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmMove, setConfirmMove] = useState(false);
  const [moving, setMoving] = useState(false);
  // CEREBRO-PATCH(comment-reminder-menu): FIR-2643 — "Remind me" surface (cerebro-inbox).
  const reminder = useCommentReminder(issueId);

  // The parent precomputes the flat thread (using collectThreadReplies),
  // memoizes by thread, and stabilizes the array reference, so we render
  // straight from `replies` instead of re-walking the graph on every render.
  const allNestedReplies = replies;

  const replyCount = allNestedReplies.length;
  const contentPreview = (entry.content ?? "").replace(/\n/g, " ").slice(0, 80);
  const reactions = entry.reactions ?? [];
  const contentText = entry.content ?? "";
  const isLongContent = contentText.length > 500 || contentText.split("\n").length > 8;
  // CEREBRO-PATCH(comment-user-question-badge): highlight comments that are user questions.
  const isQuestion = isUserQuestionComment(entry);

  const isHighlighted = highlightedCommentId === entry.id;
  const handleConfirmMove = useCallback(async () => {
    if (!onMoveToSubIssue) return;
    setMoving(true);
    try {
      await onMoveToSubIssue(entry.id);
      setConfirmMove(false);
    } catch {
      toast.error(t(($) => $.comment.move.failed_toast));
    } finally {
      setMoving(false);
    }
  }, [entry.id, onMoveToSubIssue, t]);

  // CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 select-mode plumbing.
  // `orderedIds` is the thread in render order (root first, then replies); the
  // backend re-sorts by created_at, so this is only used to send a tidy order.
  const orderedIds = useMemo(
    () => [entry.id, ...allNestedReplies.map((r) => r.id)],
    [entry.id, allNestedReplies],
  );
  const canStartNewThread = canMoveToNewThread && !!onMoveToNewThread;
  const startNewThread = useCallback((seedId: string) => {
    setSelectedIds(new Set([seedId]));
    setSelectMode(true);
  }, []);
  const toggleSelect = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);
  const cancelSelect = useCallback(() => {
    setSelectMode(false);
    setSelectedIds(new Set());
  }, []);
  const confirmMoveThread = useCallback(async () => {
    if (!onMoveToNewThread || selectedIds.size === 0) return;
    const ids = orderedIds.filter((id) => selectedIds.has(id));
    setMovingThread(true);
    try {
      await onMoveToNewThread(ids);
      setSelectMode(false);
      setSelectedIds(new Set());
    } catch {
      toast.error(t(($) => $.comment.move_thread.failed_toast));
    } finally {
      setMovingThread(false);
    }
  }, [onMoveToNewThread, selectedIds, orderedIds, t]);

  return (
    <Card
      className={cn(
        "!py-0 !gap-0 overflow-hidden transition-colors duration-700",
        isQuestion && "border-primary/35 bg-primary/[0.03]",
        isTemp && "opacity-60",
        isHighlighted && "ring-2 ring-brand/50 bg-brand/5",
      )}
      data-testid={isQuestion ? "user-question-comment" : undefined}
    >
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
        <div className="px-3 sm:px-4 py-3">
          <div className="flex items-center gap-2.5">
            <CollapsibleTrigger className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors">
              <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-90")} />
            </CollapsibleTrigger>
            {/* CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 root select checkbox. */}
            {selectMode && (
              <Checkbox
                checked={selectedIds.has(entry.id)}
                onCheckedChange={() => toggleSelect(entry.id)}
                aria-label={t(($) => $.comment.move_thread.action)}
              />
            )}
            <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size={24} enableHoverCard showStatusDot />
            {/* CEREBRO-PATCH(comment-meta-below-name-root): TECH-3032 — name row, time+cost sub-row (root). */}
            <div className="flex shrink-0 flex-col gap-0.5">
              <div className="flex items-center gap-1.5">
                <span className="cursor-pointer text-sm font-medium leading-none">
                  {getActorName(entry.actor_type, entry.actor_id)}
                </span>
                {isQuestion && (
                  <span className="inline-flex shrink-0 items-center gap-1 rounded-full border border-primary/25 bg-background px-2 py-0.5 text-[11px] font-medium text-primary">
                    <MessageCircleQuestion className="h-3 w-3" />
                    {t(($) => $.comment.question_badge)}
                  </span>
                )}
              </div>
              <div className="flex items-center gap-1.5">
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
                {open && <CommentCostBadge issueId={issueId} commentId={entry.id} authorType={entry.actor_type} />}
              </div>
            </div>

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

            {open && (
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
                    void copyText(entry.content ?? "").then((ok) => {
                      if (ok) toast.success(t(($) => $.comment.copied_toast));
                    });
                  }}>
                    <Copy className="h-3.5 w-3.5" />
                    {t(($) => $.comment.copy_action)}
                  </DropdownMenuItem>
                  {/* CEREBRO-PATCH(comment-reminder-menu): FIR-2643 — remind me about this comment (all comments). */}
                  {reminder.enabled && (
                    <DropdownMenuItem onClick={() => reminder.openReminder(entry.id, entry.content ?? "")}>
                      <BellRing className="h-3.5 w-3.5" />
                      {reminder.label}
                    </DropdownMenuItem>
                  )}
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
                  {canMoveEntry && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onClick={() => setConfirmMove(true)}>
                        <GitFork className="h-3.5 w-3.5" />
                        {t(($) => $.comment.move.action)}
                      </DropdownMenuItem>
                    </>
                  )}
                  {/* CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 start a new thread from the root. */}
                  {canStartNewThread && (isOwn || canModerate) && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onClick={() => startNewThread(entry.id)}>
                        <MessagesSquare className="h-3.5 w-3.5" />
                        {t(($) => $.comment.move_thread.action)}
                      </DropdownMenuItem>
                    </>
                  )}
                  {(canEditEntry || canDeleteEntry) && (
                    <>
                      <DropdownMenuSeparator />
                      {canEditEntry && (
                        <DropdownMenuItem onClick={edit.startEdit}>
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
              <MoveCommentToSubIssueDialog
                open={confirmMove}
                onOpenChange={setConfirmMove}
                onConfirm={handleConfirmMove}
                moving={moving}
                replyCount={replyCount}
              />
              {/* CEREBRO-PATCH(comment-reminder-menu): FIR-2643 — dialog lives outside the dropdown so it survives menu close. */}
              {reminder.dialog}
              </div>
            )}
          </div>
        </div>

        {/* Collapsible body */}
        <CollapsibleContent>
          {/* Parent comment body */}
          <div className="px-3 sm:px-4 pb-3">
            {edit.editing ? (
              <div
                {...edit.dropZoneProps}
                className="relative pl-2 sm:pl-10"
                onKeyDown={(e) => { if (e.key === "Escape") edit.cancelEdit(); }}
              >
                <div className="text-sm leading-relaxed">
                  <ContentEditor
                    ref={edit.editorRef}
                    defaultValue={edit.initialValue}
                    placeholder={t(($) => $.comment.edit_placeholder)}
                    onUpdate={(md) => {
                      if (md.trim().length > 0) edit.setDraft(edit.draftKey, md);
                      else edit.clearDraft(edit.draftKey);
                    }}
                    onSubmit={edit.saveEdit}
                    onUploadFile={edit.handleUpload}
                    debounceMs={100}
                    currentIssueId={issueId}
                  />
                </div>
                <div className="flex items-center justify-between mt-2">
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    {edit.standaloneEditAttachments.length > 0 && (
                      <AttachmentList
                        attachments={edit.standaloneEditAttachments}
                        className="max-w-full"
                        onRemove={(attachmentId) =>
                          edit.setRetainedStandaloneIds((ids) => {
                            const next = new Set(ids ?? []);
                            next.delete(attachmentId);
                            return next;
                          })
                        }
                      />
                    )}
                    {/* CEREBRO-PATCH(file-upload-button-api): onAttach/onEmbed popup picker;
                        uploads route through handleParentEditUpload so they bind on save. */}
                    <FileUploadButton
                      size="sm"
                      multiple
                      onAttach={(files) => files.forEach((f) => edit.handleUpload(f))}
                      onEmbed={(files) => files.forEach((f) => edit.editorRef.current?.uploadFile(f, { embedImage: true }))}
                    />
                  </div>
                  <div className="flex items-center gap-2">
                    <Button size="sm" variant="ghost" onClick={edit.cancelEdit}>{t(($) => $.comment.cancel_edit)}</Button>
                    <Button size="sm" variant="outline" onClick={edit.saveEdit}>{t(($) => $.comment.save_action)}</Button>
                  </div>
                </div>
                {edit.isDragOver && <FileDropOverlay />}
              </div>
            ) : (
              <>
                <div className="pl-2 sm:pl-10 text-sm leading-relaxed text-foreground/85">
                  <Suspense fallback={<p className="whitespace-pre-wrap text-sm leading-relaxed">{entry.content ?? ""}</p>}>
                    <LazyReadonlyContent content={entry.content ?? ""} attachments={entry.attachments} />
                  </Suspense>
                </div>
                <AttachmentList attachments={entry.attachments} content={entry.content} className="mt-1.5 pl-2 sm:pl-10" />
                {!isTemp && (
                  <ReactionBar
                    reactions={reactions}
                    currentUserId={currentUserId}
                    onToggle={(emoji) => onToggleReaction(entry.id, emoji)}
                    getActorName={getActorName}
                    hideAddButton={!isLongContent}
                    className="mt-1.5 pl-2 sm:pl-10"
                  />
                )}
              </>
            )}
          </div>

          {/* Replies */}
          {allNestedReplies.map((reply) => (
            <div key={reply.id} id={`comment-${reply.id}`} className={cn("border-t border-border/50 px-3 sm:px-4 transition-colors duration-700", highlightedCommentId === reply.id && "bg-brand/5")}>
              <CommentRow
                issueId={issueId}
                entry={reply}
                currentUserId={currentUserId}
                canModerate={canModerate}
                onEdit={onEdit}
                onDelete={onDelete}
                onToggleReaction={onToggleReaction}
                selectMode={selectMode}
                selected={selectedIds.has(reply.id)}
                onToggleSelect={toggleSelect}
                canStartNewThread={canStartNewThread}
                onStartNewThread={startNewThread}
              />
            </div>
          ))}

          {/* Reply input — hidden while picking; the floating action bar takes over. */}
          {selectMode ? (
            // CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 floating select-action
            // bar pinned to the viewport, so it stays reachable on long threads instead
            // of scrolling out of view at the bottom of the thread.
            <div className="pointer-events-none fixed inset-x-0 bottom-6 z-50 flex justify-center px-4">
              <div className="pointer-events-auto flex w-full max-w-[min(36rem,calc(100vw-2rem))] flex-wrap items-center justify-between gap-3 rounded-2xl border border-border bg-popover px-4 py-2.5 text-popover-foreground shadow-lg">
                <span className="text-xs text-muted-foreground">
                  {t(($) => $.comment.move_thread.select_hint)}
                </span>
                <div className="flex items-center gap-2">
                  <Button size="sm" variant="ghost" onClick={cancelSelect} disabled={movingThread}>
                    {t(($) => $.comment.cancel_action)}
                  </Button>
                  <Button
                    size="sm"
                    variant="default"
                    disabled={selectedIds.size === 0 || movingThread}
                    onClick={confirmMoveThread}
                  >
                    {movingThread
                      ? t(($) => $.comment.move_thread.moving_action)
                      : t(($) => $.comment.move_thread.move_action, { count: selectedIds.size })}
                  </Button>
                </div>
              </div>
            </div>
          ) : (
            <div className="border-t border-border/50 px-3 sm:px-4 py-2.5">
              <ReplyInput
                issueId={issueId}
                rootCommentId={entry.id} // CEREBRO-PATCH(comment-drafts): TECH-3491 — scope the reply draft to this thread.
                placeholder={t(($) => $.reply.placeholder)}
                size="sm"
                avatarType="member"
                avatarId={currentUserId ?? ""}
                // CEREBRO-PATCH(reply-target-indicator): FIR-2392 show the agent the trigger logic will actually wake (issue assignee / squad leader), not the replied-to comment author.
                triggerAgentId={triggerAgentId}
                onSubmit={(content, attachmentIds) => onReply(entry.id, content, attachmentIds)}
              />
            </div>
          )}
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
