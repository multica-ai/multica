"use client";

import * as React from "react";
import {
  ArrowLeftRight,
  Folder,
  ListChecks,
  MessageSquare,
  MoreHorizontal,
  Pin,
  Search,
  User,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import type { MemberWithUser } from "@multica/core/types";
import { firstLineTitle } from "../core";
import type { Note } from "../core";

// drag-and-drop payload type for moving a note row onto a folder.
export const NOTE_DND_TYPE = "application/x-note-id";

// A note carries an owner_id but no owner name (the wire shape is lightweight),
// so resolve the display name from the workspace member list (FIR-1460). Used by
// the list rows and the editor so you can always see who owns a note.
export type OwnerInfo = Pick<MemberWithUser, "user_id" | "name">;

export function ownerName(
  ownerId: string,
  myId: string | undefined,
  members: OwnerInfo[],
): string {
  if (ownerId && ownerId === myId) return "You";
  const m = members.find((x) => x.user_id === ownerId);
  return m?.name?.trim() || "Unknown";
}

export function previewBody(note: Note): string {
  const lines = note.body.split("\n").filter((l) => l.trim().length > 0);
  // Drop the first line when it doubled as the title, so the preview shows the
  // body rather than repeating the heading.
  const startsWithTitle = !note.title.trim() && lines.length > 0 ? 1 : 0;
  return lines.slice(startsWithTitle).join(" ") || "Empty note";
}

// FIR-4028 slice 9: a checklist is the one thing in a note body whose state a
// row can show without opening it. Markdown task syntax only — a "- [ ]" line
// written by the editor's task list or typed by hand.
const TASK_LINE = /^\s*[-*]\s+\[[ xX]\]/gm;

export function taskProgress(
  note: Pick<Note, "body">,
): { done: number; total: number } | null {
  const items = note.body.match(TASK_LINE);
  if (!items) return null;
  return {
    done: items.filter((line) => /\[[xX]\]/.test(line)).length,
    total: items.length,
  };
}

/**
 * The note one step up or down the rendered list. Clamps at both ends rather
 * than wrapping: an arrow key that silently jumps from the last note back to
 * the first reads as a glitch, not as navigation.
 */
export function stepNoteId(
  ids: string[],
  currentId: string | null,
  delta: 1 | -1,
): string | null {
  if (ids.length === 0) return null;
  const at = currentId ? ids.indexOf(currentId) : -1;
  if (at === -1) return ids[0]!;
  const next = Math.min(Math.max(at + delta, 0), ids.length - 1);
  return ids[next]!;
}

/**
 * The note search field. Up and down walk the list from inside it, so a search
 * and the note it found are one continuous movement — `preventDefault` keeps
 * the caret where it is and focus never leaves the field.
 */
export function NoteSearchField({
  value,
  onChange,
  onStep,
}: {
  value: string;
  onChange: (v: string) => void;
  onStep: (delta: 1 | -1) => void;
}) {
  return (
    <div className="relative">
      <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
          e.preventDefault();
          onStep(e.key === "ArrowDown" ? 1 : -1);
        }}
        placeholder="Search notes…"
        className="h-8 w-full pl-8"
      />
    </div>
  );
}

export function NoteListSection({
  label,
  notes,
  selectedId,
  onSelect,
  myId,
  members,
  folderNameById,
  currentFolderId,
  onMove,
}: {
  label: string;
  notes: Note[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  myId: string | undefined;
  members: OwnerInfo[];
  // FIR-4163: a row has to say which folder its note lives in — the list is
  // flat while searching, and at the root a note can sit outside the tree.
  folderNameById: Map<string, string>;
  currentFolderId: string | null;
  onMove: (note: Note) => void;
}) {
  if (notes.length === 0) return null;
  return (
    <div>
      {label && (
        <div className="px-4 pb-1.5 pt-3 text-[11px] uppercase tracking-wide text-muted-foreground">
          {label}
        </div>
      )}
      {notes.map((n) => {
        // Only worth showing when it isn't implied by where you are standing.
        const folderName =
          n.folder_id && n.folder_id !== currentFolderId
            ? folderNameById.get(n.folder_id)
            : undefined;
        const tasks = taskProgress(n);
        // One list, joined by dots — each fact decides only whether it is worth
        // showing, never which of the others came before it.
        const facts = [
          folderName && (
            <>
              <Folder className="size-3" />
              <span className="max-w-[9rem] truncate">{folderName}</span>
            </>
          ),
          // FIR-2595: per-note visibility ("Only you" / share) is removed —
          // folders drive who can open and edit a note. Show the owner when it
          // isn't you (FIR-1460, request 1).
          n.owner_id && n.owner_id !== myId && (
            <>
              <User className="size-3" />
              <span className="truncate">
                {ownerName(n.owner_id, myId, members)}
              </span>
            </>
          ),
          // FIR-4028 slice 9: how much of the note's checklist is done.
          tasks && (
            <>
              <ListChecks className="size-3" />
              <span title={`${tasks.done} of ${tasks.total} tasks done`}>
                {tasks.done}/{tasks.total}
              </span>
            </>
          ),
          // FIR-2145: comment count indicator — only shown when > 0.
          n.comment_count > 0 && (
            <>
              <MessageSquare className="size-3" />
              <span
                title={`${n.comment_count} ${n.comment_count === 1 ? "comment" : "comments"}`}
              >
                {n.comment_count}
              </span>
            </>
          ),
        ].filter(Boolean);
        return (
          <div
            key={n.id}
            className={cn(
              "group relative border-b",
              selectedId === n.id && "bg-muted",
            )}
          >
            <button
              onClick={() => onSelect(n.id)}
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData(NOTE_DND_TYPE, n.id);
                e.dataTransfer.effectAllowed = "move";
              }}
              className="block w-full cursor-grab px-4 py-2.5 pr-10 text-left hover:bg-muted/50 active:cursor-grabbing"
            >
              <div className="flex items-center gap-1.5 text-[13px] font-medium">
                {n.pinned && <Pin className="size-3 text-amber-500" />}
                <span className="truncate">{firstLineTitle(n)}</span>
              </div>
              <div className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
                {previewBody(n)}
              </div>
              <div className="mt-1.5 flex items-center gap-1 text-[11px] text-muted-foreground">
                {facts.map((fact, i) => (
                  <React.Fragment key={i}>
                    {i > 0 && <span className="opacity-50">·</span>}
                    {fact}
                  </React.Fragment>
                ))}
              </div>
            </button>
            {/* Sibling of the row button, never nested inside it. Always visible
                on touch; hover-revealed on desktop to keep the list calm. */}
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon"
                    className="absolute right-1 top-2 size-7 text-muted-foreground md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100"
                    aria-label={`Actions for ${firstLineTitle(n)}`}
                  />
                }
              >
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => onMove(n)}>
                  <ArrowLeftRight className="mr-2 size-3.5" /> Move to…
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        );
      })}
    </div>
  );
}
