"use client";

import { useState } from "react";
import { MoreHorizontal } from "lucide-react";
import { toast } from "sonner";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { Markdown } from "@/components/markdown";
import { useActorName } from "@/features/workspace";
import { timeAgo } from "@/shared/utils";
import { ReplyInput } from "./reply-input";
import type { TimelineEntry } from "@/shared/types";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface CommentCardProps {
  entry: TimelineEntry;
  allReplies: Map<string, TimelineEntry[]>;
  currentUserId?: string;
  onReply: (parentId: string, content: string) => Promise<void>;
  onEdit: (commentId: string, content: string) => Promise<void>;
  onDelete: (commentId: string) => void;
}

// ---------------------------------------------------------------------------
// Single comment row (used for both parent and replies within the same Card)
// ---------------------------------------------------------------------------

function CommentRow({
  entry,
  currentUserId,
  onEdit,
  onDelete,
}: {
  entry: TimelineEntry;
  currentUserId?: string;
  onEdit: (commentId: string, content: string) => Promise<void>;
  onDelete: (commentId: string) => void;
}) {
  const { getActorName } = useActorName();
  const [editing, setEditing] = useState(false);
  const [editContent, setEditContent] = useState("");

  const isOwn = entry.actor_type === "member" && entry.actor_id === currentUserId;
  const isTemp = entry.id.startsWith("temp-");

  const startEdit = () => {
    setEditContent(entry.content ?? "");
    setEditing(true);
  };

  const cancelEdit = () => {
    setEditing(false);
    setEditContent("");
  };

  const saveEdit = async () => {
    const trimmed = editContent.trim();
    if (!trimmed) return;
    try {
      await onEdit(entry.id, trimmed);
      setEditing(false);
      setEditContent("");
    } catch {
      toast.error("Failed to update comment");
    }
  };

  return (
    <div className={`py-3${isTemp ? " opacity-60" : ""}`}>
      <div className="flex items-center gap-2.5">
        <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size={24} />
        <span className="text-sm font-medium">
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

        {!isTemp && isOwn && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-xs" className="ml-auto text-muted-foreground">
                  <MoreHorizontal className="h-4 w-4" />
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={startEdit}>Edit</DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => onDelete(entry.id)} variant="destructive">
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>

      {editing ? (
        <form
          onSubmit={(e) => { e.preventDefault(); saveEdit(); }}
          className="mt-2 pl-8"
        >
          <input
            autoFocus
            value={editContent}
            onChange={(e) => setEditContent(e.target.value)}
            aria-label="Edit comment"
            className="w-full text-sm bg-transparent border-b border-border outline-none py-1"
            onKeyDown={(e) => { if (e.key === "Escape") cancelEdit(); }}
          />
          <div className="flex gap-2 mt-1.5">
            <Button size="sm" type="submit">Save</Button>
            <Button size="sm" variant="ghost" type="button" onClick={cancelEdit}>Cancel</Button>
          </div>
        </form>
      ) : (
        <div className="mt-1.5 pl-8 text-sm leading-relaxed text-foreground/85">
          <Markdown mode="minimal">{entry.content ?? ""}</Markdown>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// CommentCard — One Card per thread (parent + all replies flat inside)
// ---------------------------------------------------------------------------

function CommentCard({
  entry,
  allReplies,
  currentUserId,
  onReply,
  onEdit,
  onDelete,
}: CommentCardProps) {
  // Collect all nested replies recursively into a flat list
  const allNestedReplies: TimelineEntry[] = [];
  const collectReplies = (parentId: string) => {
    const children = allReplies.get(parentId) ?? [];
    for (const child of children) {
      allNestedReplies.push(child);
      collectReplies(child.id);
    }
  };
  collectReplies(entry.id);

  return (
    <Card className={`!py-0 !gap-0 overflow-hidden${entry.id.startsWith("temp-") ? " opacity-60" : ""}`}>
      {/* Parent comment */}
      <div className="px-4">
        <CommentRow
          entry={entry}
          currentUserId={currentUserId}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      </div>

      {/* Replies — flat, separated by border */}
      {allNestedReplies.map((reply) => (
        <div key={reply.id} className="border-t border-border/50 px-4">
          <CommentRow
            entry={reply}
            currentUserId={currentUserId}
            onEdit={onEdit}
            onDelete={onDelete}
          />
        </div>
      ))}

      {/* Reply input — always visible at bottom */}
      <div className="border-t border-border/50 px-4 py-2.5">
        <ReplyInput
          placeholder="Leave a reply..."
          size="sm"
          avatarType="member"
          avatarId={currentUserId ?? ""}
          onSubmit={(content) => onReply(entry.id, content)}
        />
      </div>
    </Card>
  );
}

export { CommentCard, type CommentCardProps };
