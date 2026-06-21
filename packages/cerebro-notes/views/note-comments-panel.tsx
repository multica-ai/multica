"use client";

import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { MessageSquarePlus, Check, X, Reply, Trash2, CornerDownRight, Send } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  ContentEditor,
  ReadonlyContent,
  type ContentEditorRef,
} from "@multica/views/editor";
import { Badge } from "@multica/ui/components/ui/badge";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { cn } from "@multica/ui/lib/utils";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { ApiError } from "@multica/core/api";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  useNoteComments,
  useCreateNoteComment,
  useResolveNoteComment,
  useDeleteNoteComment,
  useDecideNoteSuggestion,
  useSendNoteComments,
  useNoteReferences,
  isUnsentToAgent,
  parseIssueMentions,
  buildThreads,
  noteKeys,
  type IssueMention,
  type NoteComment,
  type NoteThread,
} from "../core";
import type { Editor } from "@tiptap/react";
import { useCommentAnchors } from "./use-comment-anchors";
import { DRAFT_ANCHOR_ID, type CommentAnchor } from "./comment-anchor-plugin";
import { NoteCoupleAndSend } from "./note-couple-and-send";
import { NoteSuggestIssueReference } from "./note-suggest-issue-reference";

// FIR-1621 — the reference `object` kinds a note can be coupled to as a send
// destination. Mirrors couplingIssue/couplingChat in the Go send handler.
const COUPLE_ISSUE = "issue";
const COUPLE_CHAT = "chat_session";

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
//
// FIR-1621 — the Documents editor (cerebro-artifacts) reuses this panel but lives
// in a package that cannot import the comment-anchor plugin. When it passes its
// live `editor`, the panel paints the highlights itself from its own comments
// query + draftQuote; the Notes page leaves `editor` undefined and keeps wiring
// the highlight at the page level (so there is no double-registration).
export function NoteCommentsPanel({
  noteId,
  noteBody,
  isOwner,
  draftQuote,
  activeAnchorId,
  onClearDraft,
  onSelectThread,
  onClose,
  editor,
}: {
  noteId: string;
  noteBody: string;
  isOwner: boolean;
  draftQuote: string | null;
  activeAnchorId: string | null;
  onClearDraft: () => void;
  onSelectThread: (id: string | null) => void;
  onClose: () => void;
  editor?: Editor | null;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const myId = useAuthStore((s: { user: { id: string } | null }) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: comments = [] } = useNoteComments(noteId);
  const create = useCreateNoteComment(noteId);

  // FIR-1621 — when the host hands us a live editor (the Documents view), paint
  // the orange comment-anchor highlights here: every root comment that carries a
  // quote, plus the in-progress draft selection. The Notes page wires this at the
  // page level instead and never passes `editor`, so `useCommentAnchors(null,…)`
  // is a no-op there — no double registration.
  const commentAnchors = React.useMemo<CommentAnchor[]>(() => {
    if (!editor) return [];
    const list: CommentAnchor[] = comments
      .filter((c) => !c.thread_root_id && c.anchor_quote)
      .map((c) => ({ id: c.id, quote: c.anchor_quote as string }));
    if (draftQuote) list.push({ id: DRAFT_ANCHOR_ID, quote: draftQuote });
    return list;
  }, [editor, comments, draftQuote]);
  const activeAnchor = draftQuote ? DRAFT_ANCHOR_ID : activeAnchorId;
  useCommentAnchors(editor ?? null, commentAnchors, activeAnchor);

  // FIR-1621 — agent-collaboration: collect unsent comments and send the
  // selected (or all) to the note's coupled issue/chat. Behind a flag; nothing
  // shows unless the flag is on.
  const collabEnabled = useFeatureFlag("cerebro_note_agent_collab");
  const send = useSendNoteComments(noteId);
  const { data: references = [] } = useNoteReferences(
    collabEnabled ? noteId : undefined,
  );
  // FIR-1753 — a note can be linked to more than one issue/chat (References
  // allows several issue links for context). The whole list of couplings is the
  // set of valid send destinations; `coupling` is the first, kept for the
  // single-coupling label + the per-comment select affordance.
  const couplings = React.useMemo(
    () =>
      references.filter(
        (r) => r.object === COUPLE_ISSUE || r.object === COUPLE_CHAT,
      ),
    [references],
  );
  const coupling = couplings[0] ?? null;
  const multiCoupled = couplings.length > 1;

  // Which coupling "Send all"/"Send selected" target when the note has more than
  // one. Defaults to the first; kept valid as the coupling list changes.
  const [destRefId, setDestRefId] = React.useState<string>("");
  React.useEffect(() => {
    if (!couplings.some((c) => c.ref_id === destRefId)) {
      setDestRefId(couplings[0]?.ref_id ?? "");
    }
  }, [couplings, destRefId]);
  const selectedCoupling =
    couplings.find((c) => c.ref_id === destRefId) ?? coupling;
  const hasIssueCoupling = couplings.some((c) => c.object === COUPLE_ISSUE);

  // FIR-1753 (point 3) — issues @-tagged inside the note's comments that are not
  // yet linked to the note. When the note has no issue coupling, we offer to add
  // the mentioned issue as a reference (which couples it). Already-linked issues
  // (by ref_id) are filtered out so a linked issue never reappears as a suggestion.
  const suggestedIssues = React.useMemo<IssueMention[]>(() => {
    if (!collabEnabled || hasIssueCoupling) return [];
    const linked = new Set(references.map((r) => r.ref_id));
    const seen = new Set<string>();
    const out: IssueMention[] = [];
    for (const c of comments) {
      for (const m of parseIssueMentions(c.body)) {
        if (linked.has(m.id) || seen.has(m.id)) continue;
        seen.add(m.id);
        out.push(m);
      }
    }
    return out;
  }, [collabEnabled, hasIssueCoupling, references, comments]);

  const [selected, setSelected] = React.useState<Set<string>>(new Set());

  const [mode, setMode] = React.useState<"comment" | "suggestion">("comment");
  const [draft, setDraft] = React.useState("");
  // FIR-1647 — the comment/suggestion composer is the mention-aware rich editor
  // (same one used in the note body), so a member can @-tag a person, agent or
  // issue. Tagging an agent is what drives "unsent" + send-to-agent — a plain
  // textarea could never produce a `mention://agent/<id>` link, so this is the
  // input side of that whole feature.
  const composerRef = React.useRef<ContentEditorRef>(null);

  const threads = React.useMemo(() => buildThreads(comments), [comments]);
  const open = threads.filter((t) => !t.root.resolved);
  const resolved = threads.filter((t) => t.root.resolved);

  const unsent = React.useMemo(
    () => comments.filter(isUnsentToAgent),
    [comments],
  );
  const unsentIds = React.useMemo(
    () => new Set(unsent.map((c) => c.id)),
    [unsent],
  );
  // Keep the selection in sync with what is still actually unsent (a sent or
  // deleted comment drops out of the selection automatically).
  const selectedUnsent = React.useMemo(
    () => [...selected].filter((id) => unsentIds.has(id)),
    [selected, unsentIds],
  );

  function toggleSelect(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function doSend(ids?: string[]) {
    send.mutate(
      {
        commentIds: ids,
        // Only pin a destination when the choice is ambiguous; a single-coupling
        // note resolves server-side without a hint.
        destination:
          multiCoupled && selectedCoupling
            ? {
                object: selectedCoupling.object,
                refId: selectedCoupling.ref_id,
              }
            : undefined,
      },
      {
        onSuccess: (res) => {
          setSelected(new Set());
          const n = res.sent.length;
          const where =
            res.destination_kind === COUPLE_CHAT ? "the chat" : "the task";
          toast.success(
            n === 1
              ? `Sent 1 comment to ${where}`
              : `Sent ${n} comments to ${where}`,
          );
        },
        onError: (err) => {
          // FIR-1753 — show the actual server reason (e.g. "linked to more than
          // one task — choose which one") instead of a generic failure, so the
          // user knows what to do. Fall back only when there is no message.
          const serverMsg =
            err instanceof ApiError && err.message ? err.message : "";
          toast.error(
            serverMsg ||
              (coupling
                ? "Couldn't send the comments. Try again."
                : "Link this note to a task or chat before sending."),
          );
        },
      },
    );
  }

  function nameFor(id: string) {
    return members.find((m) => m.user_id === id)?.name ?? "Unknown";
  }

  function submit() {
    // Read straight from the editor so a click right after typing isn't lost to
    // the onUpdate debounce.
    const text = (composerRef.current?.getMarkdown() ?? draft).trim();
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
          composerRef.current?.clearContent();
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

      {/* FIR-1753 (point 3) — a comment @-tags an issue the note isn't linked to
          yet: offer to add it as a reference (which couples the note). The memo
          already gates on collabEnabled + no existing issue coupling. */}
      {suggestedIssues.length > 0 && (
        <NoteSuggestIssueReference noteId={noteId} issues={suggestedIssues} />
      )}

      {/* FIR-1621 — unsent-comments notice + send controls. */}
      {collabEnabled && unsent.length > 0 && (
        <div className="flex flex-col gap-2 border-b border-amber-400/40 bg-amber-400/10 px-3 py-2">
          <div className="flex items-center gap-1.5 text-xs">
            <Send className="size-3.5 text-amber-600" />
            <span className="font-medium">
              {unsent.length === 1
                ? "1 comment not sent to the agent yet"
                : `${unsent.length} comments not sent to the agent yet`}
            </span>
          </div>
          {coupling ? (
            <div className="flex flex-col gap-2">
              {/* FIR-1753 — when the note is linked to more than one task/chat,
                  let the user choose where the comments go instead of failing. */}
              {multiCoupled && (
                <label className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                  Send to
                  <select
                    value={destRefId}
                    onChange={(e) => setDestRefId(e.target.value)}
                    className="min-w-0 flex-1 rounded border bg-background px-1.5 py-1 text-xs text-foreground"
                  >
                    {couplings.map((c) => (
                      <option key={c.id} value={c.ref_id}>
                        {(c.object === COUPLE_CHAT ? "chat" : "task") +
                          (c.label ? ` · ${c.label}` : "")}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  size="sm"
                  onClick={() => doSend()}
                  disabled={send.isPending}
                >
                  <Send className="size-3.5" /> Send all
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => doSend(selectedUnsent)}
                  disabled={send.isPending || selectedUnsent.length === 0}
                >
                  Send selected
                  {selectedUnsent.length > 0
                    ? ` (${selectedUnsent.length})`
                    : ""}
                </Button>
                {!multiCoupled && (
                  <span className="text-[11px] text-muted-foreground">
                    → {coupling.object === COUPLE_CHAT ? "chat" : "task"}
                    {coupling.label ? ` ${coupling.label}` : ""}
                  </span>
                )}
              </div>
            </div>
          ) : (
            <NoteCoupleAndSend
              noteId={noteId}
              noteBody={noteBody}
              unsentCount={unsent.length}
            />
          )}
        </div>
      )}

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
        <div className="rounded-md border px-2 py-1 focus-within:ring-1 focus-within:ring-ring">
          <ContentEditor
            ref={composerRef}
            defaultValue=""
            onUpdate={setDraft}
            onSubmit={submit}
            showBubbleMenu={false}
            debounceMs={150}
            placeholder={
              mode === "suggestion"
                ? "Replace the selected text with…"
                : "Write a comment… (type @ to mention a person, agent or issue)"
            }
            className="min-h-[60px] text-sm"
          />
        </div>
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
              selectable={collabEnabled && Boolean(coupling)}
              selected={selected}
              onToggleSelect={toggleSelect}
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
                  selectable={collabEnabled && Boolean(coupling)}
                  selected={selected}
                  onToggleSelect={toggleSelect}
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
  selectable,
  selected,
  onToggleSelect,
  onSelect,
  onAfterDecide,
}: {
  thread: NoteThread;
  noteId: string;
  myId: string | undefined;
  isOwner: boolean;
  nameFor: (id: string) => string;
  active: boolean;
  selectable: boolean;
  selected: Set<string>;
  onToggleSelect: (id: string) => void;
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
  // FIR-1647 — replies use the same mention-aware editor, so you can @-tag in a
  // reply too (the reviewer called out that replies couldn't tag either).
  const replyRef = React.useRef<ContentEditorRef>(null);

  function sendReply() {
    const body = (replyRef.current?.getMarkdown() ?? replyText).trim();
    if (!body) return;
    reply.mutate(
      { body, thread_root_id: root.id },
      {
        onSuccess: () => {
          setReplyText("");
          replyRef.current?.clearContent();
          setReplying(false);
        },
      },
    );
  }

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
      <CommentRow c={root} nameFor={nameFor} myId={myId} noteId={noteId} canDelete={root.author_id === myId || isOwner} onDelete={() => del.mutate(root.id)} selectable={selectable} selected={selected.has(root.id)} onToggleSelect={onToggleSelect} />

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
          <CommentRow c={r} nameFor={nameFor} myId={myId} noteId={noteId} canDelete={r.author_id === myId || isOwner} onDelete={() => del.mutate(r.id)} selectable={selectable} selected={selected.has(r.id)} onToggleSelect={onToggleSelect} />
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
        <div className="mt-2 flex items-end gap-2">
          <div className="flex-1 rounded-md border px-2 py-1 focus-within:ring-1 focus-within:ring-ring">
            <ContentEditor
              ref={replyRef}
              defaultValue=""
              onUpdate={setReplyText}
              onSubmit={sendReply}
              showBubbleMenu={false}
              debounceMs={150}
              placeholder="Reply… (type @ to mention)"
              className="min-h-[40px] text-sm"
            />
          </div>
          <Button
            size="sm"
            onClick={sendReply}
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
  selectable = false,
  selected = false,
  onToggleSelect,
}: {
  c: NoteComment;
  nameFor: (id: string) => string;
  myId: string | undefined;
  noteId: string;
  canDelete: boolean;
  onDelete: () => void;
  selectable?: boolean;
  selected?: boolean;
  onToggleSelect?: (id: string) => void;
}) {
  const unsent = isUnsentToAgent(c);
  return (
    <div>
      <div className="flex items-center gap-1.5 text-xs">
        {selectable && unsent && (
          <input
            type="checkbox"
            checked={selected}
            onClick={(e) => e.stopPropagation()}
            onChange={() => onToggleSelect?.(c.id)}
            className="size-3 shrink-0 cursor-pointer accent-amber-600"
            aria-label="Select comment to send"
          />
        )}
        <span className="font-medium">
          {c.author_id === myId ? "You" : nameFor(c.author_id)}
        </span>
        <span className="text-muted-foreground">{formatWhen(c.created_at)}</span>
        {unsent && (
          <Badge
            variant="outline"
            className="border-amber-400/60 text-[10px] text-amber-700"
          >
            Unsent
          </Badge>
        )}
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
        // FIR-1647 — render markdown so an @-mention shows as a chip, not raw
        // `[@Name](mention://…)` text.
        <ReadonlyContent content={c.body} className="mt-0.5 break-words" />
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
