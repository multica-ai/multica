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
}

function authorName(props: MessageRowProps): string {
  if (props.message.author_type === "agent") {
    return props.agent?.name ?? "Unknown agent";
  }
  return props.member?.name ?? "Unknown member";
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
  const { message, channelId, onOpenThread, disableReplyAction } = props;
  const isAgent = message.author_type === "agent";
  const name = authorName(props);
  const selfUserId = useAuthStore((s) => s.user?.id ?? null);

  const isOwnMessage =
    message.author_type === "member" &&
    selfUserId != null &&
    message.author_id === selfUserId;

  // Soft-deleted messages: placeholder. The row stays in the layout so
  // thread continuity isn't broken (replies under it still render).
  if (message.deleted_at) {
    return (
      <div className="px-4 py-2 text-sm italic text-muted-foreground">
        [message deleted]
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
}: MessageRowBodyProps) {
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
      toast.error("Message can't be empty");
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
          toast.error(err instanceof Error ? err.message : "Failed to save edit");
        },
      },
    );
  };

  const handleConfirmDelete = () => {
    deleteMut.mutate(message.id, {
      onSuccess: () => setConfirmDelete(false),
      onError: (err) => {
        toast.error(err instanceof Error ? err.message : "Failed to delete message");
      },
    });
  };

  return (
    <>
      <div className="group flex gap-3 px-4 py-2 hover:bg-muted/40 transition-colors">
        <Avatar className="h-8 w-8 shrink-0">
          {!isAgent && member?.avatar_url ? (
            <AvatarImage src={member.avatar_url} alt={name} />
          ) : null}
          <AvatarFallback className={isAgent ? "bg-purple-100 text-purple-900" : ""}>
            {isAgent ? <Bot className="h-4 w-4" /> : authorInitial(name)}
          </AvatarFallback>
        </Avatar>
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="text-sm font-medium text-foreground">{name}</span>
            {isAgent ? (
              <span className="text-xs text-muted-foreground">agent</span>
            ) : null}
            <span className="text-xs text-muted-foreground">
              {formatTime(message.created_at)}
            </span>
            {message.edited_at ? (
              <span className="text-xs text-muted-foreground">(edited)</span>
            ) : null}
            <span className="ml-auto flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
              {!disableReplyAction && onOpenThread ? (
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-6 px-2 text-xs text-muted-foreground"
                  onClick={() => onOpenThread(message.id)}
                  aria-label="Reply in thread"
                >
                  <MessageSquareReply className="mr-1 h-3 w-3" />
                  Reply
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
                        aria-label="Message actions"
                      >
                        <MoreHorizontal className="h-3.5 w-3.5" />
                      </Button>
                    }
                  />
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => setEditing(true)}>
                      <Pencil className="mr-2 h-3.5 w-3.5" />
                      Edit
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() => setConfirmDelete(true)}
                      className="text-destructive focus:text-destructive"
                    >
                      <Trash2 className="mr-2 h-3.5 w-3.5" />
                      Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : null}
            </span>
          </div>
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
                  Cancel
                </Button>
                <Button size="sm" onClick={handleSaveEdit} disabled={updateMut.isPending}>
                  {updateMut.isPending ? "Saving…" : "Save"}
                </Button>
              </div>
            </div>
          ) : (
            <div className="prose prose-sm max-w-none text-foreground">
              <ReadonlyContent content={message.content} />
            </div>
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
              className="mt-1 inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <MessageSquareReply className="h-3 w-3" />
              {message.thread_reply_count}{" "}
              {message.thread_reply_count === 1 ? "reply" : "replies"}
            </button>
          ) : null}
        </div>
      </div>

      <AlertDialog
        open={confirmDelete}
        onOpenChange={(v) => {
          if (!deleteMut.isPending) setConfirmDelete(v);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this message?</AlertDialogTitle>
            <AlertDialogDescription>
              The message will be replaced with a "[message deleted]" placeholder.
              Replies under it will still be visible.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMut.isPending}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleConfirmDelete}
              disabled={deleteMut.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {deleteMut.isPending ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
