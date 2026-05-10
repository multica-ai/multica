"use client";

import { useEffect, useRef, useState } from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { Bot, MessageSquareReply, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { ContentEditor, type ContentEditorRef, ReadonlyContent } from "../../editor";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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
import { useAuthStore } from "@multica/core/auth";
import {
  useUpdateChannelMessage,
  useDeleteChannelMessage,
} from "@multica/core/channels";
import { toast } from "sonner";
import type { ChannelMessage, MemberWithUser, Agent } from "@multica/core/types";
import { MessageReactions } from "./message-reactions";
import { MessageAttachments } from "./message-attachments";
import { useT } from "../../i18n";

interface MessageRowProps {
  message: ChannelMessage;
  channelId: string;
  member?: MemberWithUser;
  agent?: Agent;
  /** When set, clicking opens a thread side panel for this message. */
  onOpenThread?: (parentMessageId: string) => void;
  /** Inside a thread panel, "reply in thread" is meaningless on the
   * replies themselves — pass true to suppress the action. */
  disableReplyAction?: boolean;
  /** True when this message is the second-or-later in a same-author
   * burst within the group window. The list computes this; we use it to
   * suppress the avatar + author header so consecutive messages read as
   * a single block (Slack/Discord-style density). */
  isGroupContinuation?: boolean;
}

function authorName(props: MessageRowProps, fallbacks: { unknownAgent: string; unknownMember: string }): string {
  if (props.message.author_type === "agent") {
    return props.agent?.name ?? fallbacks.unknownAgent;
  }
  return props.member?.name ?? fallbacks.unknownMember;
}

function authorInitial(name: string): string {
  return name.charAt(0).toUpperCase() || "?";
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

/**
 * MessageRow renders a single channel message: avatar, author label, time,
 * markdown body, reaction chips, and (when applicable) a thread-reply
 * count badge. Soft-deleted messages render a placeholder; the row is
 * preserved in the timeline so thread continuity isn't visually broken.
 *
 * Hover affordances: the "reply in thread" + "add reaction" + actions
 * menu fade in on hover (group-hover) so the timeline stays clean at
 * rest. The actions menu is gated on whether the viewer authored the
 * message — Phase 5 only exposes Edit and Delete client-side; the
 * server-side auth check (channel admin can also delete) is the source
 * of truth, so a non-author admin still gets a 204 if they bypass the
 * UI.
 */
export function MessageRow(props: MessageRowProps) {
  const { t } = useT("channels");
  const { message, channelId, onOpenThread, disableReplyAction } = props;
  const isAgent = message.author_type === "agent";
  const name = authorName(props, {
    unknownAgent: t(($) => $.messages.unknown_agent),
    unknownMember: t(($) => $.messages.unknown_member),
  });
  const selfUserId = useAuthStore((s) => s.user?.id ?? null);

  const isOwnMessage =
    message.author_type === "member" &&
    selfUserId != null &&
    message.author_id === selfUserId;

  // Soft-deleted messages: placeholder. The row stays in the layout so
  // thread continuity isn't broken (replies under it still render).
  if (message.deleted_at) {
    return (
      <div className="px-4 py-0.5 text-sm italic text-muted-foreground">
        {t(($) => $.messages.deleted_placeholder)}
      </div>
    );
  }

  return (
    <MessageRowBody
      {...props}
      name={name}
      isAgent={isAgent}
      isOwnMessage={isOwnMessage}
      onOpenThread={onOpenThread}
      disableReplyAction={disableReplyAction}
      channelId={channelId}
    />
  );
}

interface MessageRowBodyProps extends MessageRowProps {
  name: string;
  isAgent: boolean;
  isOwnMessage: boolean;
}

function MessageRowBody({
  message,
  channelId,
  member,
  name,
  isAgent,
  isOwnMessage,
  onOpenThread,
  disableReplyAction,
  isGroupContinuation,
}: MessageRowBodyProps) {
  const { t } = useT("channels");
  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const editorRef = useRef<ContentEditorRef>(null);
  const updateMut = useUpdateChannelMessage(channelId);
  const deleteMut = useDeleteChannelMessage(channelId);

  // Cancel the edit cleanly if the message is deleted out from under
  // us by an admin while the editor is open.
  useEffect(() => {
    if (message.deleted_at) {
      setEditing(false);
    }
  }, [message.deleted_at]);

  const handleSaveEdit = () => {
    const next = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (next == null) return;
    if (next === "") {
      toast.error(t(($) => $.messages.empty_save_error));
      return;
    }
    if (next === message.content) {
      // Nothing to do — close the editor without firing a network call.
      setEditing(false);
      return;
    }
    updateMut.mutate(
      { messageId: message.id, content: next },
      {
        onSuccess: () => {
          setEditing(false);
        },
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : t(($) => $.messages.save_edit_failed));
        },
      },
    );
  };

  const handleConfirmDelete = () => {
    // Pass parent_message_id when deleting a thread reply so the
    // mutation can invalidate the parent's thread cache. Without this
    // the side panel kept showing the now-deleted reply until the
    // user manually refreshed.
    deleteMut.mutate(
      message.parent_message_id
        ? { messageId: message.id, parentMessageId: message.parent_message_id }
        : message.id,
      {
        onSuccess: () => setConfirmDelete(false),
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : t(($) => $.messages.delete_failed));
        },
      },
    );
  };

  // Slack-style density. First-in-group ("standalone") gets the full
  // avatar + name + time header and a small top gap. Continuations hide
  // the avatar (replaced by a hover-revealed timestamp in the same
  // gutter) and the name header — the body alone, indented to align
  // under the first message's body. The hover action toolbar sits in
  // the top-right corner regardless, so reply / edit / delete are
  // reachable on every row.
  const rowPadding = isGroupContinuation ? "py-0.5" : "pt-2 pb-0.5";

  const hoverActions = (
    <div className="absolute right-2 -top-3 z-10 flex items-center gap-0.5 rounded-md border border-border bg-background px-0.5 py-0.5 opacity-0 shadow-sm transition-opacity group-hover:opacity-100">
      {!disableReplyAction && onOpenThread ? (
        <Button
          size="sm"
          variant="ghost"
          className="h-6 px-2 text-xs text-muted-foreground"
          onClick={() => onOpenThread(message.id)}
          aria-label={t(($) => $.messages.reply_aria)}
        >
          <MessageSquareReply className="mr-1 h-3 w-3" />
          {t(($) => $.messages.reply)}
        </Button>
      ) : null}
      {isOwnMessage ? (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                size="sm"
                variant="ghost"
                className="h-6 w-6 p-0 text-muted-foreground"
                aria-label={t(($) => $.messages.actions_aria)}
              >
                <MoreHorizontal className="h-3.5 w-3.5" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => setEditing(true)}>
              <Pencil className="mr-2 h-3.5 w-3.5" />
              {t(($) => $.messages.edit)}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setConfirmDelete(true)}
              className="text-destructive focus:text-destructive"
            >
              <Trash2 className="mr-2 h-3.5 w-3.5" />
              {t(($) => $.messages.delete)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </div>
  );

  return (
    <>
      <div className={`group relative flex gap-3 px-4 ${rowPadding} hover:bg-muted/40 transition-colors`}>
        {isGroupContinuation ? (
          // Reserved gutter that mirrors the avatar's width so body text
          // aligns vertically with the first-in-group's body. The
          // timestamp fades in on hover so the row stays clean at rest.
          <span className="flex h-5 w-8 shrink-0 items-center justify-end pr-1 text-[10px] text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100">
            {formatTime(message.created_at)}
          </span>
        ) : (
          <Avatar className="h-8 w-8 shrink-0">
            {!isAgent && member?.avatar_url ? (
              <AvatarImage src={member.avatar_url} alt={name} />
            ) : null}
            <AvatarFallback className={isAgent ? "bg-purple-100 text-purple-900" : ""}>
              {isAgent ? <Bot className="h-4 w-4" /> : authorInitial(name)}
            </AvatarFallback>
          </Avatar>
        )}
        <div className="min-w-0 flex-1">
          {!isGroupContinuation ? (
            <div className="flex items-baseline gap-2">
              <span className="text-sm font-medium text-foreground">{name}</span>
              {isAgent ? (
                <span className="text-xs text-muted-foreground">{t(($) => $.messages.agent_label)}</span>
              ) : null}
              <span className="text-xs text-muted-foreground">
                {formatTime(message.created_at)}
              </span>
              {message.edited_at ? (
                <span className="text-xs text-muted-foreground">{t(($) => $.messages.edited)}</span>
              ) : null}
            </div>
          ) : message.edited_at ? (
            // No header on continuations, but still surface "(edited)"
            // somewhere — small inline marker after the body keeps it
            // discoverable without cluttering the row.
            <span className="ml-1 align-middle text-[10px] text-muted-foreground">{t(($) => $.messages.edited)}</span>
          ) : null}
          {editing ? (
            <div className="mt-1 space-y-2">
              <div className="rounded-md border border-input bg-background px-3 py-2 focus-within:ring-2 focus-within:ring-ring">
                <ContentEditor
                  ref={editorRef}
                  defaultValue={message.content}
                  submitOnEnter
                  onSubmit={handleSaveEdit}
                />
              </div>
              <div className="flex justify-end gap-2 text-xs">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setEditing(false)}
                  disabled={updateMut.isPending}
                >
                  {t(($) => $.messages.cancel)}
                </Button>
                <Button size="sm" onClick={handleSaveEdit} disabled={updateMut.isPending}>
                  {updateMut.isPending ? t(($) => $.messages.saving) : t(($) => $.messages.save)}
                </Button>
              </div>
            </div>
          ) : (
            <>
              {message.content ? (
                // Tailwind Typography's defaults add ~1em above and
                // below every paragraph; for a chat surface that's far
                // too generous and produces obvious "gappy" multi-line
                // messages. The `prose-p:my-0` etc. overrides collapse
                // those defaults to chat-appropriate spacing.
                <div className="prose prose-sm max-w-none leading-snug text-foreground prose-p:my-0 prose-headings:my-1 prose-pre:my-1 prose-ul:my-1 prose-ol:my-1 prose-blockquote:my-1">
                  <ReadonlyContent content={message.content} />
                </div>
              ) : null}
              <MessageAttachments attachments={message.attachments} />
            </>
          )}
          <MessageReactions
            channelId={channelId}
            messageId={message.id}
            reactions={message.reactions}
          />
          {!disableReplyAction && message.thread_reply_count > 0 && onOpenThread ? (
            <button
              type="button"
              onClick={() => onOpenThread(message.id)}
              className="mt-1 inline-flex items-center gap-1.5 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-700 hover:bg-amber-500/20 hover:text-amber-800 dark:text-amber-300 dark:hover:text-amber-200"
              data-testid="thread-reply-count"
            >
              <span className="size-1.5 rounded-full bg-amber-500" aria-hidden />
              <MessageSquareReply className="h-3 w-3" />
              {t(($) => $.messages.reply_count, { count: message.thread_reply_count })}
            </button>
          ) : null}
        </div>
        {hoverActions}
      </div>

      <AlertDialog
        open={confirmDelete}
        onOpenChange={(v) => {
          if (!deleteMut.isPending) setConfirmDelete(v);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.messages.delete_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.messages.delete_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMut.isPending}>{t(($) => $.messages.delete_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleConfirmDelete}
              disabled={deleteMut.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {deleteMut.isPending ? t(($) => $.messages.deleting) : t(($) => $.messages.delete_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
