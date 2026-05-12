// CEREBRO-PATCH(channels-thread-side-panel): JEH-1017 — Slack-style right
// slide-in panel showing a single message thread (parent + flattened
// descendants). Mounted alongside `ChannelDetail`; opens when the user
// clicks the "N replies" pill on a Slack message row.
"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { ArrowUp, Copy, Loader2, MoreHorizontal, Pencil, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useActorName } from "@multica/core/workspace/hooks";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import type { TimelineEntry } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { ReactionBar } from "@multica/ui/components/common/reaction-bar";
import { QuickEmojiPicker } from "@multica/ui/components/common/quick-emoji-picker";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  ContentEditor,
  type ContentEditorRef,
  copyMarkdown,
  useFileDropZone,
  FileDropOverlay,
} from "../../editor";
import { useSubmitOnEnter } from "@multica/cerebro-preferences/views";
import { collectThreadReplies } from "../../issues/components/thread-utils";
import { ReadonlyContent } from "../../editor";
import { AttachmentList } from "@multica/cerebro-attachments/views";
import { useT } from "../../i18n";

interface ThreadSidePanelProps {
  channelId: string;
  parentEntry: TimelineEntry | null;
  repliesByParent: Map<string, TimelineEntry[]>;
  open: boolean;
  onClose: () => void;
  onSubmit: (parentId: string, content: string, attachmentIds?: string[]) => Promise<void>;
  onEdit: (commentId: string, content: string) => Promise<void>;
  onDelete: (commentId: string) => void;
  onToggleReaction: (commentId: string, emoji: string) => void;
  currentUserId?: string;
}

export function ThreadSidePanel({
  channelId,
  parentEntry,
  repliesByParent,
  open,
  onClose,
  onSubmit,
  onEdit,
  onDelete,
  onToggleReaction,
  currentUserId,
}: ThreadSidePanelProps) {
  const flatReplies = useMemo(
    () => (parentEntry ? collectThreadReplies(parentEntry.id, repliesByParent) : []),
    [parentEntry, repliesByParent],
  );

  if (!open || !parentEntry) return null;

  return (
    <aside
      role="complementary"
      aria-label="Thread"
      className="flex h-full w-full max-w-[420px] shrink-0 flex-col border-l bg-background"
    >
      <header className="flex shrink-0 items-center justify-between border-b px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold">Thread</h2>
          <p className="text-[11px] text-muted-foreground">
            {flatReplies.length === 0
              ? "No replies yet"
              : flatReplies.length === 1
                ? "1 reply"
                : `${flatReplies.length} replies`}
          </p>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          onClick={onClose}
          aria-label="Close thread"
        >
          <X className="size-4" />
        </Button>
      </header>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <ThreadEntry
          channelId={channelId}
          entry={parentEntry}
          isRoot
          currentUserId={currentUserId}
          onEdit={onEdit}
          onDelete={onDelete}
          onToggleReaction={onToggleReaction}
        />
        {flatReplies.length > 0 && (
          <div className="flex items-center gap-3 px-4 py-2">
            <div className="h-px flex-1 bg-border" />
            <span className="text-[11px] text-muted-foreground">
              {flatReplies.length} {flatReplies.length === 1 ? "reply" : "replies"}
            </span>
            <div className="h-px flex-1 bg-border" />
          </div>
        )}
        {flatReplies.map((reply) => (
          <ThreadEntry
            key={reply.id}
            channelId={channelId}
            entry={reply}
            isRoot={false}
            currentUserId={currentUserId}
            onEdit={onEdit}
            onDelete={onDelete}
            onToggleReaction={onToggleReaction}
          />
        ))}
      </div>

      <div className="shrink-0 border-t px-4 py-3">
        <ThreadReplyInput
          channelId={channelId}
          parentId={parentEntry.id}
          onSubmit={(content, ids) => onSubmit(parentEntry.id, content, ids)}
        />
      </div>
    </aside>
  );
}

interface ThreadEntryProps {
  channelId: string;
  entry: TimelineEntry;
  isRoot: boolean;
  currentUserId?: string;
  onEdit: (commentId: string, content: string) => Promise<void>;
  onDelete: (commentId: string) => void;
  onToggleReaction: (commentId: string, emoji: string) => void;
}

function ThreadEntry({
  channelId,
  entry,
  isRoot,
  currentUserId,
  onEdit,
  onDelete,
  onToggleReaction,
}: ThreadEntryProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const editorRef = useRef<ContentEditorRef>(null);
  const cancelledRef = useRef(false);
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
    enabled: editing,
  });

  const isOwn = entry.actor_type === "member" && entry.actor_id === currentUserId;
  const canEdit = isOwn;
  const canDelete = isOwn;
  const isTemp = entry.id.startsWith("temp-");
  const reactions = entry.reactions ?? [];

  const startEdit = useCallback(() => {
    cancelledRef.current = false;
    setEditing(true);
  }, []);

  const cancelEdit = useCallback(() => {
    cancelledRef.current = true;
    setEditing(false);
  }, []);

  const saveEdit = useCallback(async () => {
    if (cancelledRef.current) return;
    const next = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!next || next === (entry.content ?? "").trim()) {
      setEditing(false);
      return;
    }
    try {
      await onEdit(entry.id, next);
      setEditing(false);
    } catch {
      toast.error(t(($) => $.comment.update_failed));
    }
  }, [entry.content, entry.id, onEdit, t]);

  return (
    <div
      className={cn(
        "group/threadrow relative flex gap-2.5 px-4 py-2 hover:bg-accent/30 transition-colors",
        isRoot && "border-b pb-3",
        isTemp && "opacity-60",
      )}
    >
      <ActorAvatar
        actorType={entry.actor_type}
        actorId={entry.actor_id}
        size={32}
        enableHoverCard
        showStatusDot
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-semibold">
            {getActorName(entry.actor_type, entry.actor_id)}
          </span>
          <span className="text-[11px] text-muted-foreground tabular-nums">
            {new Date(entry.created_at).toLocaleString(undefined, {
              hour: "2-digit",
              minute: "2-digit",
              month: "short",
              day: "numeric",
            })}
          </span>
        </div>

        {editing ? (
          <div
            {...dropZoneProps}
            className="relative mt-1"
            onKeyDown={(e) => {
              if (e.key === "Escape") cancelEdit();
            }}
          >
            <div className="text-sm leading-relaxed">
              <ContentEditor
                ref={editorRef}
                defaultValue={entry.content ?? ""}
                placeholder={t(($) => $.comment.edit_placeholder)}
                onSubmit={saveEdit}
                onUploadFile={(file) => uploadWithToast(file, { issueId: channelId })}
                debounceMs={100}
                currentIssueId={channelId}
              />
            </div>
            <div className="mt-2 flex items-center justify-between">
              <FileUploadButton
                size="sm"
                onAttach={(files) =>
                  files.forEach((f) => editorRef.current?.uploadFile(f))
                }
                onEmbed={(files) =>
                  files.forEach((f) =>
                    editorRef.current?.uploadFile(f, { embedImage: true }),
                  )
                }
              />
              <div className="flex items-center gap-2">
                <Button size="sm" variant="ghost" onClick={cancelEdit}>
                  {t(($) => $.comment.cancel_edit)}
                </Button>
                <Button size="sm" variant="outline" onClick={saveEdit}>
                  {t(($) => $.comment.save_action)}
                </Button>
              </div>
            </div>
            {isDragOver && <FileDropOverlay />}
          </div>
        ) : (
          <div className="text-sm leading-snug text-foreground/90">
            <ReadonlyContent
              content={entry.content ?? ""}
              attachments={entry.attachments}
            />
          </div>
        )}

        {!editing && (
          <AttachmentList
            attachments={entry.attachments}
            content={entry.content}
            className="mt-1"
          />
        )}

        {!editing && reactions.length > 0 && (
          <ReactionBar
            reactions={reactions}
            currentUserId={currentUserId}
            onToggle={(emoji) => onToggleReaction(entry.id, emoji)}
            getActorName={getActorName}
            hideAddButton
            className="mt-1"
          />
        )}
      </div>

      {!isTemp && !editing && (
        <div className="absolute -top-3 right-4 hidden items-center gap-0.5 rounded-md border bg-background shadow-sm group-hover/threadrow:flex">
          <QuickEmojiPicker
            onSelect={(emoji) => onToggleReaction(entry.id, emoji)}
            align="end"
          />
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  aria-label="More actions"
                >
                  <MoreHorizontal className="h-4 w-4" />
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={() => {
                  copyMarkdown(entry.content ?? "");
                  toast.success(t(($) => $.comment.copied_toast));
                }}
              >
                <Copy className="h-3.5 w-3.5" />
                {t(($) => $.comment.copy_action)}
              </DropdownMenuItem>
              {(canEdit || canDelete) && (
                <>
                  <DropdownMenuSeparator />
                  {canEdit && (
                    <DropdownMenuItem onClick={startEdit}>
                      <Pencil className="h-3.5 w-3.5" />
                      {t(($) => $.comment.edit_action)}
                    </DropdownMenuItem>
                  )}
                  {canDelete && (
                    <DropdownMenuItem
                      onClick={() => setConfirmDelete(true)}
                      variant="destructive"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                      {t(($) => $.comment.delete_action)}
                    </DropdownMenuItem>
                  )}
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      )}

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.comment.delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.comment.delete_desc)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.comment.cancel_action)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirmDelete(false);
                onDelete(entry.id);
              }}
            >
              {t(($) => $.comment.delete_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

interface ThreadReplyInputProps {
  channelId: string;
  parentId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
}

function ThreadReplyInput({ channelId, parentId, onSubmit }: ThreadReplyInputProps) {
  const user = useAuthStore((s) => s.user);
  const editorRef = useRef<ContentEditorRef>(null);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const submitOnEnter = useSubmitOnEnter();
  const [isEmpty, setIsEmpty] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const handleUpload = async (file: File) => {
    const result = await uploadWithToast(file, { issueId: channelId });
    if (result) uploadMapRef.current.set(result.link, result.id);
    return result;
  };

  const handleSend = async () => {
    const content = editorRef.current
      ?.getMarkdown()
      ?.replace(/(\n\s*)+$/, "")
      .trim();
    if (!content || submitting) return;
    const activeIds: string[] = [];
    for (const [url, id] of uploadMapRef.current) {
      if (content.includes(url)) activeIds.push(id);
    }
    setSubmitting(true);
    try {
      await onSubmit(content, activeIds.length > 0 ? activeIds : undefined);
      editorRef.current?.clearContent();
      setIsEmpty(true);
      uploadMapRef.current.clear();
    } catch {
      toast.error("Failed to send reply");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex items-start gap-2.5" {...dropZoneProps}>
      {user?.id && (
        <ActorAvatar
          actorType="member"
          actorId={user.id}
          size={28}
          className="mt-0.5 shrink-0"
        />
      )}
      <div className="relative flex min-w-0 flex-1 flex-col max-h-56">
        <div className="flex-1 min-h-0 overflow-y-auto pr-14">
          <ContentEditor
            ref={editorRef}
            placeholder="Reply…"
            onUpdate={(md) => setIsEmpty(!md.trim())}
            onSubmit={handleSend}
            onUploadFile={handleUpload}
            debounceMs={100}
            currentIssueId={channelId}
            submitOnEnter={submitOnEnter}
            // Re-key on parent change so Tiptap doesn't carry over draft text
            // when the user opens a different thread.
            key={parentId}
          />
        </div>
        <div className="absolute bottom-0 right-0 flex items-center gap-1">
          <FileUploadButton
            size="sm"
            onAttach={(files) =>
              files.forEach((f) => editorRef.current?.uploadFile(f))
            }
            onEmbed={(files) =>
              files.forEach((f) =>
                editorRef.current?.uploadFile(f, { embedImage: true }),
              )
            }
          />
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  disabled={isEmpty || submitting}
                  onClick={handleSend}
                  aria-label="Send reply"
                  className="inline-flex h-7 w-7 items-center justify-center rounded-full bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:bg-muted disabled:text-muted-foreground disabled:pointer-events-none"
                >
                  {submitting ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <ArrowUp className="h-3.5 w-3.5" />
                  )}
                </button>
              }
            />
            <TooltipContent side="top">Send reply</TooltipContent>
          </Tooltip>
        </div>
        {isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}
