"use client";

import {
  cloneElement,
  useEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
  type Ref,
  type RefObject,
} from "react";
import { cn } from "@multica/ui/lib/utils";
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

/** Whether `ref`'s content is cut off, re-checked when it resizes or web fonts finish loading. */
function useIsTruncated(ref: RefObject<HTMLElement | null>, content: ReactNode) {
  const [truncated, setTruncated] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    let active = true;
    const check = () => {
      if (active) setTruncated(isContentTruncated(el));
    };
    check();
    void document.fonts?.ready.then(check);
    const observer = new ResizeObserver(check);
    observer.observe(el);
    return () => {
      active = false;
      observer.disconnect();
    };
  }, [ref, content]);
  return truncated;
}

type Side = "top" | "bottom" | "left" | "right";
type TriggerElement = ReactElement<Record<string, unknown>>;

/**
 * Tooltip that reveals the full text only when the trigger's text is actually
 * cut off. Built on `DeferredTooltip` (shared tooltip styling and open delay):
 * nothing but the trigger mounts until the first hover, so it is cheap enough
 * for dense lists, table cells, and column headers. When the open delay
 * elapses, or the trigger takes keyboard focus, it measures the trigger and its descendants and stays closed when
 * nothing overflows. Measuring at open time keeps it correct across resizes
 * and content changes without observers.
 *
 * Keyboard users reach the full text by focusing the trigger. When the text
 * sits inside a button, link or other focusable control, wrap that control
 * instead of the text. When the text stands on its own, pass `focusable`.
 *
 * Use it for tooltips whose content repeats the trigger's own text. Tooltips
 * that add information (icon labels, disabled reasons, a URL behind a short
 * label) keep using `Tooltip`.
 */
function TruncatedTooltip({
  content,
  side = "top",
  focusable = false,
  children,
}: {
  /** Full text shown when the trigger is truncated. Empty renders the child alone. */
  content: ReactNode;
  side?: Side;
  /** Make a trigger that can't take focus on its own (plain text) a tab stop
   *  while it is cut off, with a focus ring. Text that fits stays out of the
   *  tab order. It observes the trigger's size, so leave it off in dense
   *  lists. */
  focusable?: boolean;
  /** The trigger element. Cloned with hover handlers and a ref; its own props
   *  and element type are preserved, so it must forward both. */
  children: TriggerElement;
}) {
  if (content == null || content === false || content === "") return children;
  if (focusable) {
    return (
      <FocusableWhenTruncated content={content} side={side}>
        {children}
      </FocusableWhenTruncated>
    );
  }
  return <TruncationTooltip trigger={children} content={content} side={side} />;
}

function TruncationTooltip({
  trigger,
  content,
  side,
}: {
  trigger: TriggerElement;
  content: ReactNode;
  side: Side;
}) {
  return (
    <DeferredTooltip
      trigger={trigger}
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

function FocusableWhenTruncated({
  content,
  side,
  children,
}: {
  content: ReactNode;
  side: Side;
  children: TriggerElement;
}) {
  const triggerRef = useRef<HTMLElement | null>(null);
  const truncated = useIsTruncated(triggerRef, content);
  const childRef = children.props.ref as Ref<HTMLElement> | undefined;
  const trigger = cloneElement(children, {
    ref: (el: HTMLElement | null) => {
      triggerRef.current = el;
      if (typeof childRef === "function") childRef(el);
      else if (childRef) (childRef as { current: HTMLElement | null }).current = el;
    },
    tabIndex: truncated ? 0 : children.props.tabIndex,
    // The trigger's own rounding wins over the ring's default.
    className: cn(
      "rounded-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
      children.props.className as string | undefined,
    ),
  });
  return <TruncationTooltip trigger={trigger} content={content} side={side} />;
}

export { TruncatedTooltip };
