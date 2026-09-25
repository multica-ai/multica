"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getShortcut, shortcutMatchesEvent } from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

// ---------------------------------------------------------------------------
// In-page find (Cmd/Ctrl+F) for the issue detail page.
//
// The comment timeline is virtualized, so native browser find only sees the
// handful of comments mounted in the viewport. This hook powers a real
// find-in-page bar: the host force-renders the timeline flat while find is
// open (so every comment is in the DOM), and matches are painted with the
// CSS Custom Highlight API — ranges only, never DOM mutation, so it works
// over React-rendered markdown AND the contenteditable title/description
// editors without fighting ProseMirror.
// ---------------------------------------------------------------------------

const HIGHLIGHT_NAME = "multica-find";
const ACTIVE_HIGHLIGHT_NAME = "multica-find-active";

// Feature detection, evaluated lazily per call site. On browsers without the
// CSS Custom Highlight API the bar still opens and navigates, it just paints
// no tint. Guard `CSS`/`Highlight` for SSR too (no `window`).
function highlightApiSupported(): boolean {
  return (
    typeof CSS !== "undefined" &&
    "highlights" in CSS &&
    typeof Highlight !== "undefined"
  );
}

// Element text we never search.
const SKIP_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT"]);

export interface TextMatch {
  node: Text;
  start: number;
  end: number;
}

// Walk every visible text node under `root` and return each case-insensitive
// occurrence of `query`, in document order. Matches are confined to a single
// text node — a query straddling an element boundary (e.g. across a bold run)
// is not found, which is the standard trade-off for lightweight find.
//
// Pure and DOM-only (no React / no CSS API) so it is unit-testable in jsdom.
export function collectTextMatches(root: HTMLElement, query: string): TextMatch[] {
  const matches: TextMatch[] = [];
  const needle = query.toLowerCase();
  if (!needle) return matches;

  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const parent = node.parentElement;
      if (!parent) return NodeFilter.FILTER_REJECT;
      if (SKIP_TAGS.has(parent.tagName)) return NodeFilter.FILTER_REJECT;
      if (parent.closest("[data-find-ignore]")) return NodeFilter.FILTER_REJECT;
      const value = node.nodeValue;
      if (!value || value.trim().length === 0) return NodeFilter.FILTER_REJECT;
      return NodeFilter.FILTER_ACCEPT;
    },
  });

  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const textNode = node as Text;
    const haystack = (textNode.nodeValue ?? "").toLowerCase();
    let index = haystack.indexOf(needle);
    while (index !== -1) {
      matches.push({ node: textNode, start: index, end: index + needle.length });
      index = haystack.indexOf(needle, index + needle.length);
    }
  }

  return matches;
}

function isElementVisible(el: HTMLElement | null): boolean {
  return !!el && el.getClientRects().length > 0;
}

// Bring `range` into view by driving the scroll container's scrollTop directly.
// Never native scrollIntoView: on this page it propagates to the desktop shell
// wrapper and shoves the whole view off-screen (#3929). Only scrolls when the
// match is actually outside the comfortable viewport band.
function scrollRangeIntoView(container: HTMLElement | null, range: Range): void {
  if (!container) return;
  const rects = range.getClientRects();
  const rect = rects.length > 0 ? rects[0]! : range.getBoundingClientRect();
  if (!rect || (rect.width === 0 && rect.height === 0)) return;

  const containerRect = container.getBoundingClientRect();
  const pad = 80;
  const above = rect.top < containerRect.top + pad;
  const below = rect.bottom > containerRect.bottom - pad;
  if (!above && !below) return;

  const offsetWithin = rect.top - containerRect.top + container.scrollTop;
  const target = offsetWithin - container.clientHeight / 2 + rect.height / 2;
  container.scrollTop = Math.max(0, target);
}

export interface UseInPageFindResult {
  open: boolean;
  query: string;
  /** Total number of matches for the current query. */
  matchCount: number;
  /** 0-based index of the active match, or -1 when there are none. */
  activeIndex: number;
  /** Whether the CSS Custom Highlight API is available in this browser. */
  supported: boolean;
  inputRef: React.RefObject<HTMLInputElement | null>;
  setQuery: (value: string) => void;
  openFind: () => void;
  closeFind: () => void;
  goNext: () => void;
  goPrev: () => void;
}

export function useInPageFind(options: {
  /** The scroll container whose text is searched. */
  container: HTMLElement | null;
  /**
   * A value that changes whenever searchable content changes (e.g. timeline
   * length). Triggers a match recompute after the DOM settles.
   */
  contentKey: unknown;
  /** When false, the Cmd/Ctrl+F shortcut is inert (e.g. issue still loading). */
  enabled?: boolean;
}): UseInPageFindResult {
  const { container, contentKey, enabled = true } = options;

  const [open, setOpen] = useState(false);
  const [query, setQueryState] = useState("");
  const [matchCount, setMatchCount] = useState(0);
  const [activeIndex, setActiveIndex] = useState(-1);

  const inputRef = useRef<HTMLInputElement | null>(null);
  const containerRef = useRef<HTMLElement | null>(container);
  containerRef.current = container;
  const rangesRef = useRef<Range[]>([]);
  const activeIndexRef = useRef(activeIndex);
  activeIndexRef.current = activeIndex;
  const supported = highlightApiSupported();

  const clearHighlights = useCallback(() => {
    if (!supported) return;
    CSS.highlights.delete(HIGHLIGHT_NAME);
    CSS.highlights.delete(ACTIVE_HIGHLIGHT_NAME);
  }, [supported]);

  // Paint the active match on top of the all-matches tint (higher priority)
  // and, when asked, bring it into view. Called both when the user navigates
  // and after a recompute, so the active tint always tracks live ranges — a
  // range that went stale (async re-highlight replacing text nodes) is
  // replaced even when the total match count is unchanged.
  const applyActive = useCallback(
    (ranges: Range[], index: number, scroll: boolean) => {
      const range = index >= 0 ? ranges[index] : undefined;
      // Painting the active tint is the only highlight-API-only step. Active
      // tracking and scroll-to-match must keep working on browsers without the
      // CSS Custom Highlight API, otherwise prev/next would open the bar but
      // never move the view.
      if (supported) {
        if (range) {
          const active = new Highlight(range);
          active.priority = 1;
          CSS.highlights.set(ACTIVE_HIGHLIGHT_NAME, active);
        } else {
          CSS.highlights.delete(ACTIVE_HIGHLIGHT_NAME);
        }
      }
      if (range && scroll) scrollRangeIntoView(containerRef.current, range);
    },
    [supported],
  );

  // Rebuild the match set from the live DOM. `resetActive` starts navigation
  // over at the first match (query changed); otherwise the active index is
  // preserved as content shifts underneath (streaming comment, re-highlight).
  const recompute = useCallback(
    (resetActive: boolean) => {
      const root = containerRef.current;
      if (!open || !root || query.trim().length === 0) {
        rangesRef.current = [];
        clearHighlights();
        setMatchCount(0);
        setActiveIndex(-1);
        return;
      }

      const matches = collectTextMatches(root, query);
      const ranges = matches.map((m) => {
        const range = new Range();
        range.setStart(m.node, m.start);
        range.setEnd(m.node, m.end);
        return range;
      });
      rangesRef.current = ranges;

      if (ranges.length === 0) {
        clearHighlights();
        setMatchCount(0);
        setActiveIndex(-1);
        return;
      }

      if (supported) {
        CSS.highlights.set(HIGHLIGHT_NAME, new Highlight(...ranges));
      }
      setMatchCount(ranges.length);
      const prev = activeIndexRef.current;
      const nextActive =
        resetActive || prev < 0 ? 0 : Math.min(prev, ranges.length - 1);
      setActiveIndex(nextActive);
      // Scroll only when the query changed (resetActive) — never yank the view
      // while content mutates underneath a fixed active match.
      applyActive(ranges, nextActive, resetActive);
    },
    [open, query, supported, clearHighlights, applyActive],
  );

  const recomputeRef = useRef(recompute);
  recomputeRef.current = recompute;

  // Recompute when the query changes or the bar opens/closes → restart at the
  // first match. Deferred a frame so the flat (non-virtualized) render the host
  // switches to on open has committed before we walk the DOM.
  useEffect(() => {
    const raf = requestAnimationFrame(() => recomputeRef.current(true));
    return () => cancelAnimationFrame(raf);
  }, [open, query, container]);

  // Recompute when searchable content changes, keeping the active match.
  useEffect(() => {
    if (!open) return;
    const raf = requestAnimationFrame(() => recomputeRef.current(false));
    return () => cancelAnimationFrame(raf);
  }, [contentKey, open]);

  // Async DOM churn (markdown/code highlight settling, streamed replies)
  // invalidates the ranges; re-derive them, coalescing bursts into one frame.
  // Runs regardless of highlight support so fallback counting/navigation stay
  // in sync with the live DOM.
  useEffect(() => {
    if (!open || !container) return;
    let raf = 0;
    const observer = new MutationObserver(() => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => recomputeRef.current(false));
    });
    observer.observe(container, {
      subtree: true,
      childList: true,
      characterData: true,
    });
    return () => {
      observer.disconnect();
      cancelAnimationFrame(raf);
    };
  }, [open, container]);

  // Move the active tint and scroll when the user steps between matches.
  // (Recompute handles the active tint for content-driven changes.)
  useEffect(() => {
    if (!open) return;
    applyActive(rangesRef.current, activeIndex, true);
  }, [activeIndex, open, applyActive]);

  // Global Cmd/Ctrl+F. The listener is scoped to the mounted issue detail; on
  // desktop, <Activity> cycles this effect with tab visibility, so only the
  // visible tab intercepts the key. The visibility guard is defensive.
  useEffect(() => {
    if (!enabled) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.repeat || isImeComposing(e)) return;
      if (!shortcutMatchesEvent(getShortcut("findInIssue"), e)) return;
      if (!isElementVisible(containerRef.current)) return;
      e.preventDefault();
      setOpen(true);
      requestAnimationFrame(() => {
        const input = inputRef.current;
        if (input) {
          input.focus();
          input.select();
        }
      });
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [enabled]);

  // Drop highlights on unmount so a stale tint can't linger after navigation.
  useEffect(() => clearHighlights, [clearHighlights]);

  const setQuery = useCallback((value: string) => setQueryState(value), []);
  const openFind = useCallback(() => setOpen(true), []);
  const closeFind = useCallback(() => setOpen(false), []);

  const goNext = useCallback(() => {
    setActiveIndex((prev) => {
      const n = rangesRef.current.length;
      if (n === 0) return -1;
      return prev < 0 ? 0 : (prev + 1) % n;
    });
  }, []);

  const goPrev = useCallback(() => {
    setActiveIndex((prev) => {
      const n = rangesRef.current.length;
      if (n === 0) return -1;
      return prev < 0 ? n - 1 : (prev - 1 + n) % n;
    });
  }, []);

  return {
    open,
    query,
    matchCount,
    activeIndex,
    supported,
    inputRef,
    setQuery,
    openFind,
    closeFind,
    goNext,
    goPrev,
  };
}
