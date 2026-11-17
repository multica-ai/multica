"use client";

import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { keepThreadRowAnchored } from "../utils/keep-thread-rows-anchored";

/**
 * Placeholder height for a reply that has never been measured. Kept modest so
 * an unmeasured row neither hides the viewport nor inflates the thread far
 * past its real size.
 */
const UNMEASURED_ROW_HEIGHT = 320;

/** Thread rows past this count always render behind {@link VirtualThreadRow}. */
const VIRTUALIZE_MIN_ROWS = 30;

/**
 * Thread Markdown, in characters, past which a shorter thread virtualizes too.
 * The timeline's Virtuoso treats a whole thread as one item, so a few dozen
 * long agent replies mount in one synchronous commit as soon as the thread
 * nears the viewport. MUL-9409: 28 rows / ~59k characters / ~48k px stayed
 * blank for ~0.7s on scroll-up. Rendered replies run roughly 0.8px per
 * character, so 12k characters is ~10k px — several viewports of content.
 */
const VIRTUALIZE_MIN_CONTENT_CHARS = 12_000;

/**
 * Whether a thread's replies should mount on demand. Row count alone missed
 * threads with few but very long replies, which cost the most to mount.
 */
export function shouldVirtualizeThreadRows(contentLengths: readonly number[]): boolean {
  if (contentLengths.length >= VIRTUALIZE_MIN_ROWS) return true;
  if (contentLengths.length < 2) return false;
  let total = 0;
  for (const length of contentLengths) {
    total += length;
    if (total >= VIRTUALIZE_MIN_CONTENT_CHARS) return true;
  }
  return false;
}

/**
 * Last measured height per row anchor. A row that scrolls away keeps its
 * height on the row, but the whole thread unmounts once Virtuoso drops it;
 * this lets the remounted thread reserve real heights instead of resetting
 * every reply to {@link UNMEASURED_ROW_HEIGHT}. Anchors are comment / task
 * UUIDs, so one map serves every issue; insertion order bounds it.
 */
const MAX_REMEMBERED_HEIGHTS = 2_000;
const rememberedHeights = new Map<string, number>();

function rememberHeight(id: string, height: number) {
  rememberedHeights.delete(id);
  rememberedHeights.set(id, height);
  if (rememberedHeights.size > MAX_REMEMBERED_HEIGHTS) {
    const oldest = rememberedHeights.keys().next().value;
    if (oldest !== undefined) rememberedHeights.delete(oldest);
  }
}

/** Test-only: forget every remembered height. */
export function resetRememberedThreadRowHeights() {
  rememberedHeights.clear();
}

/**
 * Keep anchors addressable without parsing every offscreen Markdown reply.
 *
 * Every timeline thread row renders through this wrapper, virtualized or not,
 * so a thread that crosses the virtualization threshold (a new long report
 * arrives) keeps the same component tree: its rows stay mounted until they are
 * observed offscreen, and an open editor is never torn down by the switch.
 * The anchor `id` lives on the wrapper only while virtualized; otherwise the
 * row content carries it as before.
 */
export function VirtualThreadRow({ id, taskId, scrollParent, virtualize = true, children }: {
  id: string;
  taskId?: string;
  scrollParent?: HTMLElement | null;
  /** False renders the row inline; switching it never remounts the row. */
  virtualize?: boolean;
  children: () => ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const height = useRef(rememberedHeights.get(id) ?? UNMEASURED_ROW_HEIGHT);
  const active = !!scrollParent && virtualize;
  // A row that starts inline counts as visible, so turning virtualization on
  // later keeps it mounted until the observer reports it offscreen.
  const [visible, setVisible] = useState(!active);
  const mounted = !active || visible;

  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    if (!active) {
      setVisible(true);
      return;
    }
    if (typeof IntersectionObserver !== "function") {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      const intersecting = entries.at(-1)?.isIntersecting ?? false;
      // Editing and open controls own transient state; don't discard it on scroll.
      const engaged = node.contains(document.activeElement)
        || !!node.querySelector('[contenteditable="true"], [aria-expanded="true"]');
      setVisible(intersecting || engaged);
    }, { root: scrollParent, rootMargin: "800px 0px" });
    observer.observe(node);
    return () => observer.disconnect();
  }, [active, scrollParent]);

  // The placeholder's height becomes the real one in this commit. Keep the
  // content below a row above the viewport from moving (see the helper).
  // Only a placeholder → content transition shifts layout.
  const wasMounted = useRef(mounted);
  useLayoutEffect(() => {
    const was = wasMounted.current;
    wasMounted.current = mounted;
    const node = ref.current;
    if (was || !mounted || !node || !scrollParent) return;
    keepThreadRowAnchored(scrollParent, node, height.current);
  }, [mounted, scrollParent]);

  useEffect(() => {
    const node = ref.current;
    if (!node || !mounted) return;
    const measure = () => {
      const next = node.getBoundingClientRect().height;
      if (next > 0) {
        height.current = next;
        rememberHeight(id, next);
      }
    };
    measure();
    if (typeof ResizeObserver !== "function") return;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [mounted, id]);

  return <div ref={ref} id={active ? id : undefined} data-run-slot-id={active ? taskId : undefined} data-thread-row=""
    data-virtual-thread-row={active ? "" : undefined} data-virtual-thread-mounted={active ? mounted : undefined}
    style={mounted ? undefined : { height: height.current }}>
    {mounted ? children() : null}
  </div>;
}
