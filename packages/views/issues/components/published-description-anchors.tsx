"use client";

import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { TimelineEntry } from "@multica/core/types";
import { locateDescriptionAnchor, type DescriptionAnchor } from "@multica/core/drafts/description-anchor";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { indexCommentText } from "./comment-annotation-selection";
import { CommentSelectionBubble } from "./comment-selection-bubble";

export function descriptionAnchorRange(root: HTMLElement, anchor: DescriptionAnchor, index = indexCommentText(root, true)): Range | null {
  const { text, nodes } = index;
  const start = locateDescriptionAnchor(text, anchor);
  if (start === null) return null;
  const end = start + anchor.quote.length;
  const first = nodes.find((n) => n.start <= start && n.end > start);
  const last = nodes.find((n) => n.start < end && n.end >= end);
  if (!first || !last) return null;
  const range = document.createRange();
  range.setStart(first.node, start - first.start);
  range.setEnd(last.node, end - last.start);
  return range;
}

function sourceFor(node: HTMLElement | null, issueId: string): HTMLElement | undefined {
  // A peek and a page can coexist. Keep navigation in the comment's own surface.
  return Array.from(node?.closest('[data-mc="issue-detail"]')?.querySelectorAll<HTMLElement>("[data-comment-content]") ?? [])
    .find((root) => root.dataset.commentContent === `description:${issueId}`);
}

/** Published markers are server data, independent of the private draft hook. */
export function PublishedDescriptionAnchors({ issueId, entries, onJumpToComment }: {
  issueId: string; entries: readonly TimelineEntry[]; onJumpToComment: (id: string) => void;
}) {
  const { t } = useT("issues");
  const ref = useRef<HTMLSpanElement>(null);
  const name = `published-description-${useId().replace(/[^a-zA-Z0-9-]/g, "")}`;
  const anchors = useMemo(() => entries.flatMap((entry) => entry.deleted_at ? [] :
    (entry.description_anchors ?? []).filter((a) => a.issue_id === issueId).map((anchor) => ({ anchor, commentId: entry.id }))), [entries, issueId]);
  const [located, setLocated] = useState<{ anchor: DescriptionAnchor; commentId: string; range: Range; root: HTMLElement }[]>([]);
  useEffect(() => {
    const root = sourceFor(ref.current, issueId);
    if (!root) return;
    const update = () => {
      const index = indexCommentText(root, true);
      const next = anchors.flatMap((item) => {
        const range = descriptionAnchorRange(root, item.anchor, index);
        return range ? [{ ...item, range, root }] : [];
      });
      setLocated(next);
      if (typeof Highlight !== "undefined" && typeof CSS !== "undefined" && CSS.highlights) CSS.highlights.set(name, new Highlight(...next.map((a) => a.range)));
    };
    update();
    const observer = new MutationObserver(update);
    observer.observe(root, { childList: true, subtree: true, characterData: true });
    const click = (event: MouseEvent) => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || !window.getSelection()?.isCollapsed) return;
      const index = indexCommentText(root, true);
      const item = anchors.find(({ anchor }) => {
        const range = descriptionAnchorRange(root, anchor, index);
        return range && Array.from(range.getClientRects()).some((rect) => event.clientX >= rect.left && event.clientX <= rect.right && event.clientY >= rect.top && event.clientY <= rect.bottom);
      });
      if (item) { event.preventDefault(); event.stopPropagation(); onJumpToComment(item.commentId); }
    };
    root.addEventListener("click", click, true);
    return () => { observer.disconnect(); root.removeEventListener("click", click, true); if (typeof CSS !== "undefined" && CSS.highlights) CSS.highlights.delete(name); };
  }, [anchors, issueId, name, onJumpToComment]);
  const ranges = located.map((a) => a.range);
  return <><span ref={ref} />
    <style>{`::highlight(${name}) { background: color-mix(in srgb, var(--brand) 25%, transparent); text-decoration: underline; }`}</style>
    {located.map(({ anchor, commentId, range, root }, index) => <CommentSelectionBubble key={`${commentId}:${anchor.id}`} range={range} source={root} owner={`published:${issueId}`} markerRanges={ranges}>
      <IconButton data-published-description-anchor={anchor.id} variant="brandSubtle" size="icon-sm"
        label={t(($) => $.reply.annotations.view_published_comment, { number: index + 1 })}
        onClick={() => onJumpToComment(commentId)}>{index + 1}</IconButton>
    </CommentSelectionBubble>)}
  </>;
}

export function DescriptionAnchorLinks({ issueId, anchors }: { issueId: string; anchors?: readonly DescriptionAnchor[] }) {
  const { t } = useT("issues");
  const ref = useRef<HTMLDivElement>(null);
  const [located, setLocated] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (!anchors?.length) return;
    const root = sourceFor(ref.current, issueId);
    if (!root) return;
    const update = () => {
      const index = indexCommentText(root, true);
      setLocated(new Set(anchors.filter((a) => a.issue_id === issueId && descriptionAnchorRange(root, a, index)).map((a) => a.id)));
    };
    update();
    const observer = new MutationObserver(update);
    observer.observe(root, { childList: true, subtree: true, characterData: true });
    return () => observer.disconnect();
  }, [anchors, issueId]);
  if (!anchors?.length) return null;
  return <div ref={ref} className="flex flex-col gap-1" data-description-anchor-links>
    {anchors.filter((a) => a.issue_id === issueId).map((anchor) => <div key={anchor.id}>
      <Button variant="ghost" size="sm" disabled={!located.has(anchor.id)} onClick={() => {
        const root = sourceFor(ref.current, issueId);
        const range = root && descriptionAnchorRange(root, anchor);
        if (!root || !range) return;
        const element = range.startContainer.parentElement;
        const container = root.closest<HTMLElement>("[data-description-anchor-scroll]");
        if (element && container) {
          const box = container.getBoundingClientRect(), target = element.getBoundingClientRect();
          container.scrollTop += target.top - box.top - (box.height - target.height) / 2;
        }
        const editor = root.querySelector<HTMLElement>('[contenteditable="true"]');
        (editor ?? root).focus({ preventScroll: true });
        // Native selection identifies the exact passage without editing it.
        const selection = window.getSelection(); selection?.removeAllRanges(); selection?.addRange(range);
      }}>{t(($) => $.reply.annotations.back_to_description)}: {anchor.quote.slice(0, 80)}</Button>
      {!located.has(anchor.id) && <span data-description-anchor-outdated className="text-caption text-muted-foreground">{t(($) => $.reply.annotations.published_outdated)}</span>}
    </div>)}
  </div>;
}
