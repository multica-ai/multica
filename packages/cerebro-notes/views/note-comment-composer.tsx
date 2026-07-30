"use client";

import * as React from "react";
import {
  computePosition,
  offset,
  flip,
  shift,
  autoUpdate,
} from "@floating-ui/dom";
import { CornerDownRight, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef } from "@multica/views/editor";
import { DRAFT_ANCHOR_ID } from "./comment-anchor-plugin";

// FIR-4139 — the comment composer used to be pinned to the bottom of the
// comments rail. Marking a line near the top of a long note put the box you
// type in hundreds of pixels away from the text you marked, so you lost sight
// of what you were commenting on.
//
// The composer is now the same UI in two shells:
//   • NoteCommentComposer — the plain composer body (mode toggle, quoted span,
//     editor, submit). Used docked at the bottom of the rail when nothing is
//     marked, and on mobile where the rail is a full-width sheet.
//   • FloatingNoteCommentComposer — the same body inside a floating card that
//     anchors to the marked span, Google-Docs style. Used whenever a selection
//     is being commented on.
//
// The anchor is the orange decoration painted by comment-anchor-plugin: the
// draft selection carries data-anchor-id="__draft__", so the card follows the
// marking itself (which survives edits) rather than a captured coordinate.

export type NoteComposerMode = "comment" | "suggestion";

export interface NoteCommentComposerProps {
  noteId: string;
  mode: NoteComposerMode;
  onModeChange: (mode: NoteComposerMode) => void;
  /** The marked text this comment attaches to, if any. */
  draftQuote: string | null;
  onClearDraft: () => void;
  /** Live markdown of the composer, mirrored so the editor can be re-seeded. */
  draft: string;
  onDraftChange: (value: string) => void;
  composerRef: React.RefObject<ContentEditorRef | null>;
  onSubmit: () => void;
  submitting: boolean;
  /** FIR-2595 point 3 — scope the @mention picker to people with note access. */
  scopedMentions: boolean;
  autoFocus?: boolean;
}

export function NoteCommentComposer({
  noteId,
  mode,
  onModeChange,
  draftQuote,
  onClearDraft,
  draft,
  onDraftChange,
  composerRef,
  onSubmit,
  submitting,
  scopedMentions,
  autoFocus = false,
}: NoteCommentComposerProps) {
  return (
    <>
      <div className="mb-2 flex gap-1">
        <Button
          size="sm"
          variant={mode === "comment" ? "secondary" : "ghost"}
          onClick={() => onModeChange("comment")}
        >
          Comment
        </Button>
        <Button
          size="sm"
          variant={mode === "suggestion" ? "secondary" : "ghost"}
          onClick={() => onModeChange("suggestion")}
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
          // Re-seeded when the composer moves between the docked and the
          // floating shell (React remounts it), so typed text is not lost.
          defaultValue={draft}
          onUpdate={onDraftChange}
          onSubmit={onSubmit}
          showBubbleMenu={false}
          debounceMs={150}
          autoFocus={autoFocus}
          currentNoteId={scopedMentions ? noteId : undefined}
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
          onClick={onSubmit}
          disabled={
            submitting ||
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
    </>
  );
}

// Rect of the marked span, or null when the marking is not currently painted
// (the quote could not be located in the live document).
function draftAnchorRect(): DOMRect | null {
  if (typeof document === "undefined") return null;
  const el = document.querySelector<HTMLElement>(
    `[data-anchor-id="${DRAFT_ANCHOR_ID}"]`,
  );
  return el ? el.getBoundingClientRect() : null;
}

export function FloatingNoteCommentComposer({
  onDismiss,
  fallbackRef,
  ...composerProps
}: NoteCommentComposerProps & {
  /** Escape / the card's close button — clears the draft selection. */
  onDismiss: () => void;
  /**
   * Anchored to the marked span; when that span is not painted (quote not
   * locatable in the live document) the card falls back to this element — the
   * comments rail — so it never lands in a corner detached from the note.
   */
  fallbackRef: React.RefObject<HTMLElement | null>;
}) {
  const cardRef = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    const card = cardRef.current;
    if (!card) return;
    const virtual = {
      getBoundingClientRect: () =>
        draftAnchorRect() ??
        fallbackRef.current?.getBoundingClientRect() ??
        new DOMRect(0, 0, 0, 0),
    };
    const update = () => {
      void computePosition(virtual, card, {
        strategy: "fixed",
        placement: "bottom-start",
        middleware: [offset(8), flip({ padding: 8 }), shift({ padding: 8 })],
      }).then(({ x, y }) => {
        if (!card.isConnected) return;
        card.style.left = `${x}px`;
        card.style.top = `${y}px`;
      });
    };
    // animationFrame: the marked span is a ProseMirror decoration that is
    // rebuilt on every document change, so the reference element can be
    // replaced under us — a per-frame recompute keeps the card glued to it.
    return autoUpdate(virtual, card, update, { animationFrame: true });
  }, [fallbackRef]);

  return (
    <div
      ref={cardRef}
      role="dialog"
      aria-label="Add a comment on the selected text"
      data-testid="floating-comment-composer"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onDismiss();
        }
      }}
      style={{ position: "fixed", top: 0, left: 0, zIndex: 50 }}
      className={cn(
        "w-[22rem] max-w-[calc(100vw-1rem)] rounded-lg border bg-popover p-3",
        "shadow-lg ring-1 ring-orange-400/30",
      )}
    >
      <NoteCommentComposer {...composerProps} />
    </div>
  );
}
