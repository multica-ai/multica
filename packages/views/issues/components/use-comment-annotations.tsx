"use client";

import { useEffect, useId, useRef, useState } from "react";
import { Popover } from "@base-ui/react/popover";
import { MessageSquarePlus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import { MAX_ANNOTATION_QUOTE_LENGTH, MAX_REPLY_ANNOTATIONS } from "@multica/core/drafts/reply-annotation";
import type { TimelineEntry } from "@multica/core/types";
import { useT } from "../../i18n";
import { annotationRange, captureCommentSelection, findAnnotationSource } from "./comment-annotation-selection";

type CapturedSelection = NonNullable<ReturnType<typeof captureCommentSelection>>;

export function useCommentAnnotations({ draftKey, entry, replies, enabled, getActorName }: {
  draftKey: CommentDraftKey;
  entry: TimelineEntry;
  replies: TimelineEntry[];
  enabled: boolean;
  getActorName: (type: string, id: string) => string;
}) {
  const { t } = useT("issues");
  const cardRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [selection, setSelection] = useState<CapturedSelection | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [error, setError] = useState(false);
  const annotations = useCommentDraftStore((s) => s.getAnnotations(draftKey));
  const anchorsRef = useRef(annotations);
  anchorsRef.current = annotations;
  const anchorKey = annotations.map((a) => a.id).join(",");
  const editing = annotations.find((a) => a.id === editingId);
  const highlightName = `reply-annotation-${useId().replace(/[^a-zA-Z0-9-]/g, "")}`;

  const close = (restoreFocus = false) => {
    if (restoreFocus && selection?.root.isConnected) selection.root.focus({ preventScroll: true });
    setSelection(null); setEditingId(null); setError(false);
  };
  const capture = () => {
    if (!enabled || !cardRef.current) return;
    const captured = captureCommentSelection(cardRef.current, window.getSelection());
    const source = captured && [entry, ...replies].find((e) => e.id === captured.sourceCommentId);
    if (!captured || source?.actor_type !== "agent" || source.type !== "comment" ||
      (source.comment_type && source.comment_type !== "comment")) return;
    setSelection(captured);
    setEditingId(null);
    setError(false);
  };
  const actionRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (editingId && window.matchMedia("(pointer: fine)").matches) textareaRef.current?.focus();
  }, [editingId]); // Only on opening; typing must not reset the caret.

  useEffect(() => {
    if (!cardRef.current || typeof Highlight === "undefined" || !CSS.highlights) return;
    const card = cardRef.current;
    const update = () => {
      const ranges = anchorsRef.current.flatMap((a) => {
        const source = findAnnotationSource(card, a.sourceCommentId);
        const range = source && annotationRange(source, a);
        return range ? [range] : [];
      });
      CSS.highlights.set(highlightName, new Highlight(...ranges));
    };
    update();
    const observer = new MutationObserver((records) => {
      // Typing in the reply editor must not re-index a long source comment.
      if (records.some((record) => {
        const target = record.target instanceof Element ? record.target : record.target.parentElement;
        return target?.closest("[data-comment-content]") ||
          [...record.addedNodes, ...record.removedNodes].some((node) => node instanceof Element &&
            (node.matches("[data-comment-content]") || node.querySelector("[data-comment-content]")));
      })) update();
    });
    observer.observe(card, { childList: true, subtree: true, characterData: true });
    return () => { observer.disconnect(); CSS.highlights.delete(highlightName); };
  }, [anchorKey, highlightName]);

  const add = () => {
    if (!selection) return;
    const source = [entry, ...replies].find((e) => e.id === selection.sourceCommentId);
    if (!source) { close(); return; }
    const { quote, start, prefix, suffix, sourceCommentId } = selection;
    const id = useCommentDraftStore.getState().addAnnotation(draftKey, {
      id: crypto.randomUUID(), sourceCommentId,
      sourceActorName: source.actor_name || getActorName(source.actor_type, source.actor_id),
      sourceRevision: source.revision, quote, start, prefix, suffix, note: "",
    });
    if (!id) { setError(true); return; }
    setEditingId(id);
    window.getSelection()?.removeAllRanges();
  };

  return {
    cardRef,
    captureProps: {
      "data-annotation-thread": entry.id,
      onPointerUp: (event: React.PointerEvent) => {
        if (event.target instanceof Element && event.target.closest("[data-comment-content]") &&
          !event.target.closest("button, [contenteditable=true]")) capture();
      },
      onKeyUp: (event: React.KeyboardEvent) => {
        if (event.shiftKey && event.key.startsWith("Arrow")) capture();
      },
      onKeyDown: (event: React.KeyboardEvent) => {
        if (event.key === "Tab" && !event.shiftKey && selection && !editingId &&
          event.target instanceof Element && event.target.closest("[data-comment-content]")) {
          event.preventDefault();
          actionRef.current?.focus();
        }
      },
    },
    popup: <>
      {annotations.length > 0 && <style>{`::highlight(${highlightName}) { background: color-mix(in srgb, var(--brand) 20%, transparent); }`}</style>}
      <Popover.Root open={!!selection} onOpenChange={(open) => { if (!open) close(); }}>
        <Popover.Portal>
          <Popover.Positioner
            anchor={selection ? { getBoundingClientRect: () => selection.range.getBoundingClientRect(), contextElement: selection.root } : undefined}
            side="bottom" align="start" sideOffset={6} className="z-50"
          >
            <Popover.Popup initialFocus={false} finalFocus={false}
              aria-label={editing ? t(($) => $.reply.annotations.added) : t(($) => $.reply.annotations.add)}
              onKeyDown={(event) => { if (event.key === "Escape") close(true); }}
              className="max-w-[calc(100vw-24px)] rounded-lg bg-surface-raised p-2.5 text-body text-popover-foreground shadow-[var(--menu-shadow)] ring-1 ring-surface-border outline-hidden">
              {editing ? <div className="flex w-80 max-w-full flex-col gap-2.5">
                <Popover.Title className="font-medium">{t(($) => $.reply.annotations.added)}</Popover.Title>
                <blockquote className="max-h-28 overflow-y-auto whitespace-pre-wrap break-words border-l-2 border-brand/50 pl-2.5 text-muted-foreground">{editing.quote}</blockquote>
                <label className="flex flex-col gap-1.5">
                  <span className="text-label text-muted-foreground">{t(($) => $.reply.annotations.note_label)}</span>
                  <Textarea ref={textareaRef} value={editing.note}
                    onChange={(event) => useCommentDraftStore.getState().updateAnnotation(draftKey, editing.id, event.target.value)} />
                </label>
                <div className="flex items-center justify-between gap-2">
                  <span className="text-caption text-muted-foreground" role="status">{t(($) => $.reply.annotations.saved)}</span>
                  <Button size="sm" onClick={() => close(true)}>{t(($) => $.reply.annotations.done)}</Button>
                </div>
              </div> : <>
                <Button ref={actionRef} variant="ghost" size="sm" onClick={add}>
                  <MessageSquarePlus />{t(($) => $.reply.annotations.add)}
                </Button>
                {error && <p role="alert" className="max-w-72 pt-2 text-caption text-destructive">
                  {selection && selection.quote.length > MAX_ANNOTATION_QUOTE_LENGTH
                    ? t(($) => $.reply.annotations.quote_limit, { count: MAX_ANNOTATION_QUOTE_LENGTH })
                    : t(($) => $.reply.annotations.count_limit, { count: MAX_REPLY_ANNOTATIONS })}
                </p>}
              </>}
            </Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>
    </>,
  };
}
