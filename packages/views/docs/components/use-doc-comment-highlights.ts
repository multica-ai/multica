"use client";
import { useEffect, useRef, useState, type RefObject } from "react";
import { indexCommentText } from "../../issues/components/comment-annotation-selection";
import { docAnchorRange, type DocAnchor } from "./doc-comment-anchors";
import { watchDom } from "./dom-throttle";

export interface AnchoredThread extends DocAnchor {
  id: string;
}

/** Highlight names styled in packages/ui/styles/base.css. */
const HIGHLIGHT = "doc-comment";
const ACTIVE_HIGHLIGHT = "doc-comment-active";

function canHighlight() {
  return typeof Highlight !== "undefined" && typeof CSS !== "undefined" && !!CSS.highlights;
}

/**
 * Paints each open discussion's quoted text in the doc and keeps the ranges
 * current while the doc is edited. Returns which discussions could not be
 * found (their text was edited away) and a lookup from a DOM point to the
 * discussion it sits in, for clicking a highlight.
 */
export function useDocCommentHighlights(
  rootRef: RefObject<HTMLElement | null>,
  threads: AnchoredThread[],
  activeId: string | null,
) {
  const rangesRef = useRef(new Map<string, Range>());
  const [missing, setMissing] = useState<ReadonlySet<string>>(new Set());
  const threadsKey = threads.map((t) => `${t.id}:${t.quote}`).join("\n");
  const threadsRef = useRef(threads);
  threadsRef.current = threads;

  useEffect(() => {
    const host = rootRef.current;
    if (!host) return;
    const paint = () => {
      const root = host.querySelector<HTMLElement>(".ProseMirror");
      if (!root) return;
      const ranges = new Map<string, Range>();
      const gone = new Set<string>();
      const index = indexCommentText(root, true);
      for (const thread of threadsRef.current) {
        const range = docAnchorRange(root, thread, index);
        if (range) ranges.set(thread.id, range);
        else gone.add(thread.id);
      }
      rangesRef.current = ranges;
      setMissing((prev) =>
        prev.size === gone.size && [...gone].every((id) => prev.has(id)) ? prev : gone,
      );
      if (canHighlight()) {
        const active = activeId ? ranges.get(activeId) : undefined;
        CSS.highlights.set(HIGHLIGHT, new Highlight(...[...ranges].filter(([id]) => id !== activeId).map(([, r]) => r)));
        if (active) CSS.highlights.set(ACTIVE_HIGHLIGHT, new Highlight(active));
        else CSS.highlights.delete(ACTIVE_HIGHLIGHT);
      }
    };
    const stop = watchDom(host, paint);
    return () => {
      stop();
      if (canHighlight()) {
        CSS.highlights.delete(HIGHLIGHT);
        CSS.highlights.delete(ACTIVE_HIGHLIGHT);
      }
    };
  }, [rootRef, threadsKey, activeId]);

  /** The discussion whose quoted text contains this point, if any. */
  const threadAt = (node: Node, offset: number): string | null => {
    for (const [id, range] of rangesRef.current) {
      try {
        if (range.isPointInRange(node, offset)) return id;
      } catch {
        // A point in another document or a detached node is in no range.
      }
    }
    return null;
  };

  /** Scrolls the discussion's quoted text into view. */
  const reveal = (id: string) => {
    const range = rangesRef.current.get(id);
    const target = range?.startContainer.parentElement;
    target?.scrollIntoView({ block: "center", behavior: "smooth" });
  };

  return { missing, threadAt, reveal };
}
