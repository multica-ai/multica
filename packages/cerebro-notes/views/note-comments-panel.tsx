"use client";

import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { MessageSquarePlus, Check, X, Reply, Trash2, CornerDownRight } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Badge } from "@multica/ui/components/ui/badge";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  useNoteComments,
  useCreateNoteComment,
  useResolveNoteComment,
  useDeleteNoteComment,
  useDecideNoteSuggestion,
  buildThreads,
  noteKeys,
  type NoteComment,
  type NoteThread,
} from "../core";

const ANCHOR_CONTEXT = 32;

// anchorContext locates the quote in the body and grabs up to ANCHOR_CONTEXT
// chars on each side, so a comment survives later edits (matches the server's
// quote + prefix/suffix relocation in comments.go).
function anchorContext(body: string, quote: string) {
  const i = body.indexOf(quote);
  if (i < 0) return { prefix: "", suffix: "", start: null, end: null };
  return {
    prefix: body.slice(Math.max(0, i - ANCHOR_CONTEXT), i),
    suffix: body.slice(i + quote.length, i + quote.length + ANCHOR_CONTEXT),
    start: i,
    end: i + quote.length,
  };
}

// NoteCommentsPanel (Wave 3 / G1): the margin. Lists comment + suggestion
// threads for a note, lets anyone who can see the note comment/suggest on a
// text selection, reply, and resolve; the note owner can accept/reject a
// suggestion (which splices into the body server-side).
//
// The text selection being commented on (`draftQuote`) and which thread is
// "active" (`activeAnchorId`) are owned by NoteEditor so the orange highlight
// in the body and the panel stay in sync (TECH-3637).
export function NoteCommentsPanel({
  noteId,
  noteBody,
  isOwner,
  draftQuote,
  activeAnchorId,
  onClearDraft,
  onSelectThread,
  onClose,
}: {
  noteId: string;
  noteBody: string;
  isOwner: boolean;
  draftQuote: string | null;
  activeAnchorId: string | null;
  onClearDraft: () => void;
  onSelectThread: (id: string | null) => void;
  onClose: () => void;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const myId = useAuthStore((s: { user: { id: string } | null }) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: comments = [] } = useNoteComments(noteId);
  const create = useCreateNoteComment(noteId);

  const [mode, setMode] = React.useState<"comment" | "suggestion">("comment");
  const [draft, setDraft] = React.useState("");

  const threads = React.useMemo(() => buildThreads(comments), [comments]);
  const open = threads.filter((t) => !t.root.resolved);
  const resolved = threads.filter((t) => t.root.resolved);

  function nameFor(id: string) {
    return members.find((m) => m.user_id === id)?.name ?? "Unknown";
  }

  function submit() {
    const text = draft.trim();
    if (!text) return;
    const ctx = draftQuote ? anchorContext(noteBody, draftQuote) : null;
    create.mutate(
      {
        kind: mode,
        body: mode === "suggestion" ? "Suggested edit" : text,
        suggestion_text: mode === "suggestion" ? text : undefined,
        anchor_quote: draftQuote || undefined,
        anchor_prefix: ctx?.prefix || undefined,
        anchor_suffix: ctx?.suffix || undefined,
        anchor_start: ctx?.start ?? undefined,
        anchor_end: ctx?.end ?? undefined,
      },
      {
        onSuccess: () => {
          setDraft("");
          onClearDraft();
        },
      },
    );
  }

  return (
    <div className="flex h-full w-full min-w-0 flex-col">
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <MessageSquarePlus className="size-4 text-muted-foreground" />
        <span className="text-sm font-semibold">Comments</span>
        <Badge variant="outline" className="text-[10px]">
          {open.length} open
        </Badge>
        <Button size="sm" variant="ghost" className="ml-auto" onClick={onClose}>
          <X className="size-4" />
        </Button>
      </div>

      {/* Composer */}
      <div className="border-b p-3">
        <div className="mb-2 flex gap-1">
          <Button
            size="sm"
            variant={mode === "comment" ? "secondary" : "ghost"}
            onClick={() => setMode("comment")}
          >
            Comment
          </Button>
          <Button
            size="sm"
            variant={mode === "suggestion" ? "secondary" : "ghost"}
            onClick={() => setMode("suggestion")}
          >
            Suggest edit
          </Button>
        </div>
        {draftQuote ? (
          <div className="mb-2 flex items-start gap-1 rounded border border-orange-400/60 bg-orange-400/10 px-2 py-1 text-xs">
            <CornerDownRight className="mt-0.5 inline size-3 shrink-0 text-orange-600" />
            <span className="line-clamp-2 flex-1">“{draftQuote}”</span>
            <button
              type="button"
              onClick={onClearDraft}
              className="shrink-0 text-muted-foreground hover:text-destructive"
              aria-label="Clear selection"
            >
              <X className="size-3" />
            </button>
          </div>
        ) : (
          <p className="mb-2 rounded border border-dashed px-2 py-1.5 text-xs text-muted-foreground">
            Select text in the note — a “Comment” button appears above the
            selection to attach it here.
          </p>
        )}
        <Textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={
            mode === "suggestion"
              ? "Replace the selected text with…"
              : "Write a comment…"
          }
          className="min-h-[60px] text-sm"
        />
        <div className="mt-2 flex items-center justify-end gap-2">
          <Button
            size="sm"
            onClick={submit}
            disabled={
              create.isPending ||
              !draft.trim() ||
              (mode === "suggestion" && !draftQuote)
            }
          >
            {mode === "suggestion" ? "Suggest" : "Comment"}
          </Button>
        </div>
        {mode === "suggestion" && !draftQuote && (
          <p className="mt-1 text-[11px] text-muted-foreground">
            A suggestion needs attached text to replace.
          </p>
        )}
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <div className="divide-y">
          {open.length === 0 && resolved.length === 0 && (
            <p className="p-4 text-sm text-muted-foreground">
              No comments yet. Select text and add one.
            </p>
          )}
          {open.map((t) => (
            <ThreadView
              key={t.root.id}
              thread={t}
              noteId={noteId}
              myId={myId}
              isOwner={isOwner}
              nameFor={nameFor}
              active={activeAnchorId === t.root.id}
              onSelect={() =>
                onSelectThread(t.root.anchor_quote ? t.root.id : null)
              }
              onAfterDecide={() =>
                qc.invalidateQueries({ queryKey: noteKeys.all(wsId) })
              }
            />
          ))}
          {resolved.length > 0 && (
            <details className="bg-muted/20">
              <summary className="cursor-pointer px-3 py-2 text-xs text-muted-foreground">
                {resolved.length} resolved
              </summary>
              {resolved.map((t) => (
                <ThreadView
                  key={t.root.id}
                  thread={t}
                  noteId={noteId}
                  myId={myId}
                  isOwner={isOwner}
                  nameFor={nameFor}
                  active={activeAnchorId === t.root.id}
                  onSelect={() =>
                    onSelectThread(t.root.anchor_quote ? t.root.id : null)
                  }
                  onAfterDecide={() =>
                    qc.invalidateQueries({ queryKey: noteKeys.all(wsId) })
                  }
                />
              ))}
            </details>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

function ThreadView({
  thread,
  noteId,
  myId,
  isOwner,
  nameFor,
  active,
  onSelect,
  onAfterDecide,
}: {
  thread: NoteThread;
  noteId: string;
  myId: string | undefined;
  isOwner: boolean;
  nameFor: (id: string) => string;
  active: boolean;
  onSelect: () => void;
  onAfterDecide: () => void;
}) {
  const { root, replies } = thread;
  const resolve = useResolveNoteComment(noteId);
  const del = useDeleteNoteComment(noteId);
  const decide = useDecideNoteSuggestion(noteId);
  const reply = useCreateNoteComment(noteId);
  const [replyText, setReplyText] = React.useState("");
  const [replying, setReplying] = React.useState(false);

  const isSuggestion = root.kind === "suggestion";
  const pending = isSuggestion && root.suggestion_state === "pending";

  return (
    <div
      onClick={onSelect}
      className={cn(
        "p-3",
        root.resolved && "opacity-70",
        root.anchor_quote && "cursor-pointer",
        active && "bg-orange-400/10",
      )}
    >
      {root.anchor_quote && (
        <div
          className={cn(
            "mb-1.5 border-l-2 pl-2 text-xs italic line-clamp-2",
            active
              ? "border-orange-500 text-orange-700"
              : "border-amber-500/60 text-muted-foreground",
          )}
        >
          “{root.anchor_quote}”
        </div>
      )}
      <CommentRow c={root} nameFor={nameFor} myId={myId} noteId={noteId} canDelete={root.author_id === myId || isOwner} onDelete={() => del.mutate(root.id)} />

      {isSuggestion && (
        <div className="mt-1.5 rounded bg-emerald-500/10 p-2 text-sm">
          <span className="text-[11px] font-medium text-emerald-700">
            Proposes:
          </span>{" "}
          {root.suggestion_text}
          {root.suggestion_state !== "pending" && (
            <Badge
              variant="outline"
              className={cn(
                "ml-2 text-[10px]",
                root.suggestion_state === "accepted"
                  ? "text-emerald-700"
                  : "text-destructive",
              )}
            >
              {root.suggestion_state}
            </Badge>
          )}
        </div>
      )}

      {replies.map((r) => (
        <div key={r.id} className="mt-2 pl-4">
          <CommentRow c={r} nameFor={nameFor} myId={myId} noteId={noteId} canDelete={r.author_id === myId || isOwner} onDelete={() => del.mutate(r.id)} />
        </div>
      ))}

      <div className="mt-2 flex flex-wrap items-center gap-2">
        {pending && isOwner && (
          <>
            <Button
              size="sm"
              variant="secondary"
              onClick={() =>
                decide.mutate(
                  { commentId: root.id, state: "accepted" },
                  { onSuccess: onAfterDecide },
                )
              }
              disabled={decide.isPending}
            >
              <Check className="size-3.5" /> Accept
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() =>
                decide.mutate({ commentId: root.id, state: "rejected" })
              }
              disabled={decide.isPending}
            >
              <X className="size-3.5" /> Reject
            </Button>
          </>
        )}
        <Button
          size="sm"
          variant="ghost"
          onClick={() => setReplying((v) => !v)}
        >
          <Reply className="size-3.5" /> Reply
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={() =>
            resolve.mutate({ commentId: root.id, resolved: !root.resolved })
          }
          disabled={resolve.isPending}
        >
          {root.resolved ? "Reopen" : "Resolve"}
        </Button>
      </div>

      {replying && (
        <div className="mt-2 flex gap-2">
          <Textarea
            value={replyText}
            onChange={(e) => setReplyText(e.target.value)}
            placeholder="Reply…"
            className="min-h-[40px] text-sm"
          />
          <Button
            size="sm"
            onClick={() =>
              reply.mutate(
                { body: replyText.trim(), thread_root_id: root.id },
                {
                  onSuccess: () => {
                    setReplyText("");
                    setReplying(false);
                  },
                },
              )
            }
            disabled={reply.isPending || !replyText.trim()}
          >
            Send
          </Button>
        </div>
      )}
    </div>
  );
}

function CommentRow({
  c,
  nameFor,
  myId,
  canDelete,
  onDelete,
}: {
  c: NoteComment;
  nameFor: (id: string) => string;
  myId: string | undefined;
  noteId: string;
  canDelete: boolean;
  onDelete: () => void;
}) {
  return (
    <div>
      <div className="flex items-center gap-1.5 text-xs">
        <span className="font-medium">
          {c.author_id === myId ? "You" : nameFor(c.author_id)}
        </span>
        <span className="text-muted-foreground">{formatWhen(c.created_at)}</span>
        {canDelete && (
          <button
            onClick={onDelete}
            className="ml-auto text-muted-foreground hover:text-destructive"
            aria-label="Delete comment"
          >
            <Trash2 className="size-3" />
          </button>
        )}
      </div>
      {c.body && c.body !== "Suggested edit" && (
        <p className="mt-0.5 whitespace-pre-wrap break-words text-sm">{c.body}</p>
      )}
    </div>
  );
}

function formatWhen(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
