"use client";

import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { MessageSquarePlus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import { MAX_ANNOTATION_QUOTE_LENGTH, MAX_REPLY_ANNOTATIONS } from "@multica/core/drafts/reply-annotation";
import { useT } from "../../i18n";
import { annotationRange, captureCommentSelection, findAnnotationSource } from "./comment-annotation-selection";
import { CommentSelectionBubble } from "./comment-selection-bubble";

type CapturedSelection = NonNullable<ReturnType<typeof captureCommentSelection>>;
type SourceAnchor = { id: string; range: Range; root: HTMLElement };

export function useCommentAnnotations({ draftKey, sources, enabled, onAdded, editable = false }: {
  draftKey: CommentDraftKey;
  sources: { id: string; name: string; revision?: number }[];
  enabled: boolean;
  onAdded?: () => void;
  /** Editable descriptions use their existing toolbar to call addSelection. */
  editable?: boolean;
}) {
  const { t } = useT("issues");
  const cardRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const actionRef = useRef<HTMLButtonElement>(null);
  const [selection, setSelection] = useState<CapturedSelection | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [error, setError] = useState(false);
  const [anchors, setAnchors] = useState<SourceAnchor[]>([]);
  const markerRanges = useMemo(() => {
    const groups = new Map<HTMLElement, Range[]>();
    for (const anchor of anchors) {
      const group = groups.get(anchor.root) ?? [];
      group.push(anchor.range); groups.set(anchor.root, group);
    }
    return groups;
  }, [anchors]);
  const annotations = useCommentDraftStore((s) => s.getAnnotations(draftKey));
  const annotationsRef = useRef(annotations);
  annotationsRef.current = annotations;
  const sourcesRef = useRef(sources);
  sourcesRef.current = sources;
  const onAddedRef = useRef(onAdded);
  onAddedRef.current = onAdded;
  const anchorKey = annotations.map((a) => a.id).join(",");
  const editing = annotations.find((a) => a.id === editingId);
  const highlightName = `reply-annotation-${useId().replace(/[^a-zA-Z0-9-]/g, "")}`;

  const close = (restoreFocus = false) => {
    if (restoreFocus && selection?.root.isConnected) {
      const target = editable ? selection.root.querySelector<HTMLElement>("[contenteditable=true]") ?? selection.root : selection.root;
      target.focus({ preventScroll: true });
    }
    setSelection(null); setEditingId(null); setError(false);
  };
  const readSelection = useCallback(() => {
    if (!enabled || !cardRef.current) return null;
    const captured = captureCommentSelection(cardRef.current, window.getSelection(), editable);
    return captured && sourcesRef.current.some((source) => source.id === captured.sourceCommentId) ? captured : null;
  }, [enabled, editable]);
  const capture = () => {
    const captured = readSelection();
    if (captured) { setSelection(captured); setEditingId(null); setError(false); }
  };

  useEffect(() => { setSelection(null); setEditingId(null); setError(false); }, [draftKey]);

  // Dismiss on the next press, never on the click completing the opening drag.
  useEffect(() => {
    if (!selection) return;
    const onPointerDown = (event: PointerEvent) => {
      if (event.target instanceof Element &&
        event.target.closest("[data-reply-annotation-overlay]")?.getAttribute("data-reply-annotation-overlay") === draftKey) return;
      setSelection(null); setEditingId(null); setError(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (selection.root.isConnected) selection.root.focus({ preventScroll: true });
      setSelection(null); setEditingId(null); setError(false);
    };
    const onFocusIn = (event: FocusEvent) => {
      if (!(event.target instanceof Element) || selection.root.contains(event.target) ||
        event.target.closest("[data-reply-annotation-overlay]")?.getAttribute("data-reply-annotation-overlay") === draftKey) return;
      setSelection(null); setEditingId(null); setError(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("focusin", onFocusIn);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("focusin", onFocusIn);
    };
  }, [selection, draftKey]);

  useEffect(() => {
    if (editingId && window.matchMedia("(pointer: fine)").matches) inputRef.current?.focus({ preventScroll: true });
  }, [editingId]);

  useEffect(() => {
    if (!anchorKey) {
      setAnchors((current) => current.length ? [] : current);
      return;
    }
    if (!enabled || !cardRef.current) return;
    const card = cardRef.current;
    const canHighlight = typeof Highlight !== "undefined" && typeof CSS !== "undefined" && !!CSS.highlights;
    const update = () => {
      const next = annotationsRef.current.flatMap((a) => {
        const root = findAnnotationSource(card, a.sourceCommentId);
        const range = root && annotationRange(root, a, editable);
        return root && range ? [{ id: a.id, range, root }] : [];
      });
      setAnchors((current) => !current.length && !next.length ? current : next);
      // Expanding a resolved thread may remount the selected comment's body.
      // Keep the note editor attached to its saved quote, never a detached Range.
      setSelection((current) => {
        if (!current || current.root.isConnected) return current;
        const saved = annotationsRef.current.find((a) => a.sourceCommentId === current.sourceCommentId &&
          a.start === current.start && a.quote === current.quote);
        const anchor = saved && next.find((a) => a.id === saved.id);
        return anchor ? { ...current, root: anchor.root, range: anchor.range } : null;
      });
      if (canHighlight) CSS.highlights.set(highlightName, new Highlight(...next.map((a) => a.range)));
    };
    update();
    const observer = new MutationObserver((records) => {
      // Reply typing must not re-index a long source comment.
      if (records.some((record) => {
        const target = record.target instanceof Element ? record.target : record.target.parentElement;
        return target?.closest("[data-comment-content]") ||
          [...record.addedNodes, ...record.removedNodes].some((node) => node instanceof Element &&
            (node.matches("[data-comment-content]") || node.querySelector("[data-comment-content]")));
      })) update();
    });
    observer.observe(card, { childList: true, subtree: true, characterData: true });
    return () => { observer.disconnect(); if (canHighlight) CSS.highlights.delete(highlightName); };
  }, [anchorKey, highlightName, draftKey, editable, enabled]);

  useEffect(() => {
    if (editingId && !editing) { setSelection(null); setEditingId(null); }
  }, [editingId, editing]);

  const editAnnotation = (id: string, scrollToSource = false): boolean => {
    const annotation = annotationsRef.current.find((a) => a.id === id);
    const root = cardRef.current && annotation && findAnnotationSource(cardRef.current, annotation.sourceCommentId);
    const range = root && annotation && annotationRange(root, annotation, editable);
    if (!root || !range || !annotation) return false;
    if (scrollToSource) {
      const element = range.startContainer instanceof Element ? range.startContainer : range.startContainer.parentElement;
      element?.scrollIntoView({ block: "center", behavior: "instant" });
    }
    setSelection({ ...annotation, range, root }); setEditingId(id); setError(false);
    return true;
  };

  const add = useCallback((captured: CapturedSelection | null) => {
    const source = captured && sourcesRef.current.find((e) => e.id === captured.sourceCommentId);
    if (!captured || !source) {
      toast.error(t(($) => $.reply.annotations.selection_failed));
      return false;
    }
    const { quote, start, prefix, suffix, sourceCommentId } = captured;
    const id = useCommentDraftStore.getState().addAnnotation(draftKey, {
      id: crypto.randomUUID(), sourceCommentId,
      sourceActorName: source.name,
      sourceRevision: source.revision, quote, start, prefix, suffix, note: "",
    });
    if (!id) {
      setError(true);
      toast.error(captured.quote.length > MAX_ANNOTATION_QUOTE_LENGTH
        ? t(($) => $.reply.annotations.quote_limit, { count: MAX_ANNOTATION_QUOTE_LENGTH })
        : t(($) => $.reply.annotations.count_limit, { count: MAX_REPLY_ANNOTATIONS }));
      return false;
    }
    setSelection(captured);
    setEditingId(id);
    setError(false);
    window.getSelection()?.removeAllRanges();
    onAddedRef.current?.();
    return true;
  }, [draftKey, t]);
  const addSelection = useCallback(() => add(readSelection()), [add, readSelection]);

  return {
    cardRef,
    editAnnotation,
    addSelection,
    captureProps: {
      "data-annotation-thread": draftKey,
      onPointerUp: (event: React.PointerEvent) => {
        if (!editable && event.target instanceof Element && event.target.closest("[data-comment-content]") &&
          !event.target.closest("button, [contenteditable=true]")) capture();
      },
      onKeyUp: (event: React.KeyboardEvent) => {
        if (!editable && event.shiftKey && event.key.startsWith("Arrow")) capture();
      },
      onKeyDown: (event: React.KeyboardEvent) => {
        if (event.key === "Tab" && !event.shiftKey && selection && !editingId &&
          event.target instanceof Element && event.target.closest("[data-comment-content]")) {
          event.preventDefault(); actionRef.current?.focus();
        }
      },
    },
    popup: <>
      {annotations.length > 0 && <style>{`::highlight(${highlightName}) { background: color-mix(in srgb, var(--brand) 20%, transparent); }`}</style>}
      {enabled && anchors.map((anchor) => {
        const index = annotations.findIndex((a) => a.id === anchor.id);
        if (index < 0) return null;
        return <CommentSelectionBubble key={anchor.id} range={anchor.range} source={anchor.root} owner={draftKey}
          markerRanges={markerRanges.get(anchor.root)}>
          <Button variant="brandSubtle" size="icon-sm" className="size-6 rounded-full text-caption"
            aria-label={t(($) => $.reply.annotations.edit, { number: index + 1 })}
            onClick={() => editAnnotation(anchor.id)}>{index + 1}</Button>
        </CommentSelectionBubble>;
      })}
      {enabled && selection && <CommentSelectionBubble range={selection.range} source={selection.root} owner={draftKey}>
        <div className="bubble-menu max-w-[calc(100vw-16px)]">
          {editing ? <><textarea ref={inputRef} value={editing.note} rows={Math.min(4, Math.max(1, editing.note.split("\n").length))}
            aria-label={t(($) => $.reply.annotations.note_label)}
            placeholder={t(($) => $.reply.annotations.note_placeholder)}
            className="min-h-8 w-72 min-w-0 resize-none rounded-sm bg-transparent px-2 py-1 text-body outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring"
            onChange={(event) => useCommentDraftStore.getState().updateAnnotation(draftKey, editing.id, event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.nativeEvent.isComposing && !event.shiftKey && !event.metaKey && !event.ctrlKey) {
                event.preventDefault(); close(true);
              }
            }} />
            <Button variant="ghost" size="icon-sm" className="self-start text-muted-foreground hover:text-destructive"
              aria-label={t(($) => $.reply.annotations.remove, { number: annotations.indexOf(editing) + 1 })}
              title={t(($) => $.reply.annotations.remove, { number: annotations.indexOf(editing) + 1 })}
              onClick={() => {
                useCommentDraftStore.getState().removeAnnotation(draftKey, editing.id);
                close(true);
              }}><Trash2 /></Button>
            </> : <Button ref={actionRef} variant="ghost" size="sm"
              onMouseDown={(event) => event.preventDefault()} onClick={() => add(selection)}>
              <MessageSquarePlus />{t(($) => editable ? $.reply.annotations.add_comment : $.reply.annotations.add)}
            </Button>}
        </div>
        {error && <p role="alert" className="mt-1 max-w-72 rounded-lg bg-popover p-2 text-caption text-destructive shadow-[var(--menu-shadow)]">
          {selection.quote.length > MAX_ANNOTATION_QUOTE_LENGTH
            ? t(($) => $.reply.annotations.quote_limit, { count: MAX_ANNOTATION_QUOTE_LENGTH })
            : t(($) => $.reply.annotations.count_limit, { count: MAX_REPLY_ANNOTATIONS })}
        </p>}
      </CommentSelectionBubble>}
    </>,
  };
}
