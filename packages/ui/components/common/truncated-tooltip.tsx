"use client";

import type { ReactElement, ReactNode } from "react";
import { DeferredTooltip } from "./deferred-tooltip";

/**
 * Whether `node` hides part of its content. Overflow only counts on an axis
 * that actually clips (`overflow` other than `visible`): an emoji icon that
 * spills out of its fixed-size box is still fully visible, so it is not
 * truncation. Covers `truncate` (horizontal) and `line-clamp-*` (vertical).
 */
const CLIPPING_OVERFLOW = new Set(["hidden", "clip", "auto", "scroll"]);

function clipsContent(node: Element): boolean {
  const overflowX = node.scrollWidth > node.clientWidth;
  const overflowY = node.scrollHeight > node.clientHeight;
  if (!overflowX && !overflowY) return false;
  const style = getComputedStyle(node);
  return (
    (overflowX && CLIPPING_OVERFLOW.has(style.overflowX)) ||
    (overflowY && CLIPPING_OVERFLOW.has(style.overflowY))
  );
}

/** True when `el` or any element inside it cuts off part of its content. */
export function isContentTruncated(el: HTMLElement | null): boolean {
  if (!el) return false;
  if (clipsContent(el)) return true;
  for (const node of el.querySelectorAll("*")) {
    if (clipsContent(node)) return true;
  }
  return false;
}

/**
 * Tooltip that reveals the full text only when the trigger's text is actually
 * cut off. Built on `DeferredTooltip` (shared tooltip styling and open delay):
 * nothing but the trigger mounts until the first hover, so it is cheap enough
 * for dense lists, table cells, and column headers. When the open delay
 * elapses, or the trigger takes keyboard focus, it measures the trigger and its descendants and stays closed when
 * nothing overflows. Measuring at open time keeps it correct across resizes
 * and content changes without observers.
 *
 * Use it for tooltips whose content repeats the trigger's own text. Tooltips
 * that add information (icon labels, disabled reasons, a URL behind a short
 * label) keep using `Tooltip`.
 */
function TruncatedTooltip({
  content,
  side = "top",
  children,
}: {
  /** Full text shown when the trigger is truncated. Empty renders the child alone. */
  content: ReactNode;
  side?: "top" | "bottom" | "left" | "right";
  /** The trigger element. Cloned with hover handlers and a ref; its own props
   *  and element type are preserved, so it must forward both. */
  children: ReactElement<Record<string, unknown>>;
}) {
  if (content == null || content === false || content === "") return children;
  return (
    <DeferredTooltip
      trigger={children}
      content={content}
      side={side}
      shouldOpen={isContentTruncated}
      openOnFocus
      // The full text is often one long token (hash, URL, path); let it wrap
      // inside the max-width bubble instead of spilling out of it.
      contentClassName="[overflow-wrap:anywhere]"
    />
  );
}

export { TruncatedTooltip };
