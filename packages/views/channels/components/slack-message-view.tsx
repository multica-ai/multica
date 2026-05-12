// CEREBRO-PATCH(channels-slack-message-view): JEH-1017 — Slack-style message
// renderer for channels and DMs. Compact rows with avatar/name on the first
// message of a group, indented continuation rows for follow-ups from the same
// author within GROUP_WINDOW_MS, date separators between days, and a thread
// pill under any message that has replies. Top-level messages only; replies
// live in the ThreadSidePanel.
"use client";

import { memo, useCallback, useMemo, useRef, useState } from "react";
import { Copy, MessageSquare, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
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
import { ReactionBar } from "@multica/ui/components/common/reaction-bar";
import { QuickEmojiPicker } from "@multica/ui/components/common/quick-emoji-picker";
import { AttachmentList } from "@multica/cerebro-attachments/views";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import type { TimelineEntry } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  ContentEditor,
  type ContentEditorRef,
  copyMarkdown,
  ReadonlyContent,
  useFileDropZone,
  FileDropOverlay,
} from "../../editor";
import { useT } from "../../i18n";

const GROUP_WINDOW_MS = 5 * 60 * 1000;

interface SlackMessageViewProps {
  channelId: string;
  topLevel: TimelineEntry[];
  /**
   * Direct children only — used to count replies and timestamp the latest
   * reply for the pill. The full thread tree is walked again inside the side
   * panel via `collectThreadReplies`.
   */
  repliesByParent: Map<string, TimelineEntry[]>;
  currentUserId?: string;
  activeThreadId?: string | null;
  onOpenThread: (parentId: string) => void;
  onEdit: (commentId: string, content: string) => Promise<void>;
  onDelete: (commentId: string) => void;
  onToggleReaction: (commentId: string, emoji: string) => void;
}

export function SlackMessageView({
  channelId,
  topLevel,
  repliesByParent,
  currentUserId,
  activeThreadId,
  onOpenThread,
  onEdit,
  onDelete,
  onToggleReaction,
}: SlackMessageViewProps) {
  const grouped = useMemo(() => groupByDay(topLevel), [topLevel]);

  return (
    <div className="flex flex-col">
      {grouped.map((day) => (
        <div key={day.dayKey}>
          <DateSeparator dayKey={day.dayKey} />
          {day.entries.map((entry, idx) => {
            const prev = idx > 0 ? day.entries[idx - 1] : null;
            const continuation = !!prev && shouldGroupWith(prev, entry);
            return (
              <MessageRow
                key={entry.id}
                channelId={channelId}
                entry={entry}
                continuation={continuation}
                threadCount={countDescendants(entry.id, repliesByParent)}
                threadLastTs={latestDescendantTs(entry.id, repliesByParent)}
                isThreadOpen={entry.id === activeThreadId}
                currentUserId={currentUserId}
                onOpenThread={onOpenThread}
                onEdit={onEdit}
                onDelete={onDelete}
                onToggleReaction={onToggleReaction}
              />
            );
          })}
        </div>
      ))}
    </div>
  );
}

interface MessageRowProps {
  channelId: string;
  entry: TimelineEntry;
  continuation: boolean;
  threadCount: number;
  threadLastTs: string | null;
  isThreadOpen: boolean;
  currentUserId?: string;
  onOpenThread: (parentId: string) => void;
  onEdit: (commentId: string, content: string) => Promise<void>;
  onDelete: (commentId: string) => void;
  onToggleReaction: (commentId: string, emoji: string) => void;
}

const MessageRow = memo(function MessageRow({
  channelId,
  entry,
  continuation,
  threadCount,
  threadLastTs,
  isThreadOpen,
  currentUserId,
  onOpenThread,
  onEdit,
  onDelete,
  onToggleReaction,
}: MessageRowProps) {
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

  const isOwn =
    entry.actor_type === "member" && entry.actor_id === currentUserId;
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
        "group/msg relative flex gap-2.5 px-4 py-0.5 hover:bg-accent/30 transition-colors",
        continuation ? "pt-0" : "pt-2",
        isTemp && "opacity-60",
        isThreadOpen && "bg-accent/40",
      )}
    >
      <div className="w-9 shrink-0">
        {!continuation && (
          <ActorAvatar
            actorType={entry.actor_type}
            actorId={entry.actor_id}
            size={36}
            enableHoverCard
            showStatusDot
          />
        )}
        {continuation && (
          <Tooltip>
            <TooltipTrigger
              render={
                <span className="block pl-1 pt-0.5 text-[10px] text-muted-foreground/0 group-hover/msg:text-muted-foreground tabular-nums">
                  {formatHourMinute(entry.created_at)}
                </span>
              }
            />
            <TooltipContent side="top">
              {new Date(entry.created_at).toLocaleString()}
            </TooltipContent>
          </Tooltip>
        )}
      </div>

      <div className="min-w-0 flex-1">
        {!continuation && (
          <div className="flex items-baseline gap-2">
            <span className="text-sm font-semibold">
              {getActorName(entry.actor_type, entry.actor_id)}
            </span>
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="text-[11px] text-muted-foreground tabular-nums">
                    {formatHourMinute(entry.created_at)}
                  </span>
                }
              />
              <TooltipContent side="top">
                {new Date(entry.created_at).toLocaleString()}
              </TooltipContent>
            </Tooltip>
          </div>
        )}

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

        {!editing && !isTemp && reactions.length > 0 && (
          <ReactionBar
            reactions={reactions}
            currentUserId={currentUserId}
            onToggle={(emoji) => onToggleReaction(entry.id, emoji)}
            getActorName={getActorName}
            hideAddButton
            className="mt-1"
          />
        )}

        {threadCount > 0 && (
          <button
            type="button"
            onClick={() => onOpenThread(entry.id)}
            className={cn(
              "mt-1 inline-flex items-center gap-1.5 rounded-md border border-transparent px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:border-border hover:bg-background",
              isThreadOpen && "border-border bg-background text-foreground",
            )}
            aria-label={`Open thread with ${threadCount} replies`}
          >
            <MessageSquare className="size-3" />
            <span className="font-medium">
              {threadCount} {threadCount === 1 ? "reply" : "replies"}
            </span>
            {threadLastTs && (
              <span className="text-muted-foreground/70">
                · last {formatHourMinute(threadLastTs)}
              </span>
            )}
          </button>
        )}
      </div>

      {!isTemp && !editing && (
        <div className="absolute -top-3 right-4 hidden items-center gap-0.5 rounded-md border bg-background shadow-sm group-hover/msg:flex">
          <QuickEmojiPicker
            onSelect={(emoji) => onToggleReaction(entry.id, emoji)}
            align="end"
          />
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  onClick={() => onOpenThread(entry.id)}
                  aria-label="Reply in thread"
                />
              }
            >
              <MessageSquare className="h-4 w-4" />
            </TooltipTrigger>
            <TooltipContent side="top">Reply in thread</TooltipContent>
          </Tooltip>
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
              {threadCount > 0
                ? t(($) => $.comment.delete_desc_with_replies)
                : t(($) => $.comment.delete_desc)}
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
});

function DateSeparator({ dayKey }: { dayKey: string }) {
  return (
    <div className="sticky top-0 z-10 flex items-center gap-3 px-4 py-2 backdrop-blur-sm bg-background/80">
      <div className="h-px flex-1 bg-border" />
      <span className="rounded-full border bg-background px-2.5 py-0.5 text-[11px] font-semibold text-muted-foreground">
        {formatDayLabel(dayKey)}
      </span>
      <div className="h-px flex-1 bg-border" />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Helpers — pure, exported for tests.
// ---------------------------------------------------------------------------

interface DayBucket {
  dayKey: string;
  entries: TimelineEntry[];
}

export function groupByDay(entries: TimelineEntry[]): DayBucket[] {
  const out: DayBucket[] = [];
  let current: DayBucket | null = null;
  for (const e of entries) {
    const key = dayKey(e.created_at);
    if (!current || current.dayKey !== key) {
      current = { dayKey: key, entries: [e] };
      out.push(current);
    } else {
      current.entries.push(e);
    }
  }
  return out;
}

export function shouldGroupWith(prev: TimelineEntry, next: TimelineEntry): boolean {
  if (prev.actor_type !== next.actor_type) return false;
  if (prev.actor_id !== next.actor_id) return false;
  const dt =
    new Date(next.created_at).getTime() - new Date(prev.created_at).getTime();
  return dt >= 0 && dt < GROUP_WINDOW_MS;
}

function countDescendants(
  rootId: string,
  repliesByParent: Map<string, TimelineEntry[]>,
): number {
  let n = 0;
  const walk = (id: string) => {
    const children = repliesByParent.get(id) ?? [];
    for (const c of children) {
      n++;
      walk(c.id);
    }
  };
  walk(rootId);
  return n;
}

function latestDescendantTs(
  rootId: string,
  repliesByParent: Map<string, TimelineEntry[]>,
): string | null {
  let latest: string | null = null;
  const walk = (id: string) => {
    const children = repliesByParent.get(id) ?? [];
    for (const c of children) {
      if (latest === null || c.created_at > latest) latest = c.created_at;
      walk(c.id);
    }
  };
  walk(rootId);
  return latest;
}

function dayKey(iso: string): string {
  // Local-day bucket. ISO strings carry UTC; converting via Date keeps the
  // separator aligned with the user's clock.
  const d = new Date(iso);
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

function formatHourMinute(iso: string): string {
  const d = new Date(iso);
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

function formatDayLabel(dayKey: string): string {
  const [yStr, mStr, dayStr] = dayKey.split("-");
  const y = Number(yStr);
  const m = Number(mStr);
  const day = Number(dayStr);
  if (!Number.isFinite(y) || !Number.isFinite(m) || !Number.isFinite(day)) {
    return dayKey;
  }
  const target = new Date(y, m - 1, day);
  const today = startOfDay(new Date());
  const targetStart = startOfDay(target);
  const diffDays = Math.round(
    (targetStart.getTime() - today.getTime()) / 86400000,
  );
  if (diffDays === 0) return "Today";
  if (diffDays === -1) return "Yesterday";
  return target.toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: target.getFullYear() === today.getFullYear() ? undefined : "numeric",
  });
}

function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}
