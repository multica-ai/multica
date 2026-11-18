"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getShortcut, shortcutMatchesEvent } from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

// In-page find (Cmd/Ctrl+F) for the issue detail page.
//
// Native browser find only sees the comments the virtualized timeline has
// mounted. This hook searches the rest of the page's DOM directly and, for the
// timeline, the text of every comment from data (`virtual` below), so opening
// find never mounts the whole history (MUL-9538). Stepping to a match in an
// unmounted comment asks the host to mount just that comment, then paints it.
// Matches are painted with the CSS Custom Highlight API — ranges only, never
// DOM mutation, so it works over React-rendered markdown AND the
// contenteditable title/description editors without fighting ProseMirror.

const HIGHLIGHT_NAME = "multica-find";
const ACTIVE_HIGHLIGHT_NAME = "multica-find-active";

/** Marks the root of a searchable body inside the virtual region. */
export const FIND_UNIT_ATTRIBUTE = "data-find-unit";

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
// `[data-find-ignore]` subtrees and `skip` are left out whole.
//
// Pure and DOM-only (no React / no CSS API) so it is unit-testable in jsdom.
export function collectTextMatches(root: HTMLElement, query: string, skip?: Element | null): TextMatch[] {
  const matches: TextMatch[] = [];
  const needle = query.toLowerCase();
  if (!needle) return matches;

  const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      if (node.nodeType === Node.ELEMENT_NODE) {
        const el = node as Element;
        if (el === skip || SKIP_TAGS.has(el.tagName) || el.hasAttribute("data-find-ignore")) {
          return NodeFilter.FILTER_REJECT;
        }
        return NodeFilter.FILTER_SKIP;
      }
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

/** Non-overlapping occurrences of `needle` in `text`, both already lower-cased. */
export function countOccurrences(text: string, needle: string): number {
  if (!needle) return 0;
  let count = 0;
  for (let index = text.indexOf(needle); index !== -1; index = text.indexOf(needle, index + needle.length)) {
    count++;
  }
  return count;
}

function toRange(match: TextMatch): Range {
  const range = new Range();
  range.setStart(match.node, match.start);
  range.setEnd(match.node, match.end);
  return range;
}

function isElementVisible(el: HTMLElement | null): boolean {
  return !!el && el.getClientRects().length > 0;
}

// Bring `rect` into view by driving the scroll container's scrollTop directly.
// Never native scrollIntoView: on this page it propagates to the desktop shell
// wrapper and shoves the whole view off-screen (#3929). Only scrolls when the
// match is actually outside the comfortable viewport band.
function scrollRectIntoView(container: HTMLElement | null, rect: DOMRect | undefined): void {
  if (!container || !rect || (rect.width === 0 && rect.height === 0)) return;

  const containerRect = container.getBoundingClientRect();
  const pad = 80;
  const above = rect.top < containerRect.top + pad;
  const below = rect.bottom > containerRect.bottom - pad;
  if (!above && !below) return;

  const offsetWithin = rect.top - containerRect.top + container.scrollTop;
  const target = offsetWithin - container.clientHeight / 2 + Math.min(rect.height, container.clientHeight) / 2;
  container.scrollTop = Math.max(0, target);
}

function rangeRect(range: Range): DOMRect | undefined {
  const rects = range.getClientRects();
  return rects.length > 0 ? rects[0]! : range.getBoundingClientRect();
}

/** Searchable text that is not necessarily in the DOM. */
export interface InPageFindUnit {
  /** Matches the unit root's `data-find-unit`; ids only, never quotes. */
  key: string;
  /** Lower-cased text, as {@link collectTextMatches} would see it rendered. */
  text: string;
}

export interface InPageFindVirtualContent<U extends InPageFindUnit> {
  /** The element rendering the units; the DOM walk leaves it out. */
  root: () => HTMLElement | null;
  /** Units in reading order. A new array means the content changed. */
  units: () => readonly U[];
  /**
   * Bring `unit` into the DOM, rooted at `[data-find-unit={unit.key}]`, then
   * call `done`. Returns a cancel for when the reader moves on first.
   */
  reveal: (unit: U, done: () => void) => () => void;
}

type FindMatch = { range: Range } | { unit: number; occurrence: number };

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

export function useInPageFind<U extends InPageFindUnit = InPageFindUnit>(options: {
  /** The scroll container whose text is searched. */
  container: HTMLElement | null;
  /**
   * A value that changes whenever searchable content changes (e.g. timeline
   * length). Triggers a match recompute after the DOM settles.
   */
  contentKey: unknown;
  /** When false, the Cmd/Ctrl+F shortcut is inert (e.g. issue still loading). */
  enabled?: boolean;
  /** Content searched from data instead of the DOM (the virtualized timeline). */
  virtual?: InPageFindVirtualContent<U>;
}): UseInPageFindResult {
  const { container, contentKey, enabled = true, virtual } = options;

  const [open, setOpen] = useState(false);
  const [query, setQueryState] = useState("");
  const [matchCount, setMatchCount] = useState(0);
  const [activeIndex, setActiveIndex] = useState(-1);
  // Bumped when a new query restarts at the first match, so the activation
  // effect scrolls to it even when the index itself did not change.
  const [restart, setRestart] = useState(0);

  const inputRef = useRef<HTMLInputElement | null>(null);
  const containerRef = useRef<HTMLElement | null>(container);
  containerRef.current = container;
  const virtualRef = useRef(virtual);
  virtualRef.current = virtual;
  const matchesRef = useRef<FindMatch[]>([]);
  const unitsRef = useRef<readonly U[]>([]);
  // DOM ranges of the units that are mounted, by unit index.
  const mountedRef = useRef(new Map<number, Range[]>());
  const cancelRevealRef = useRef<(() => void) | null>(null);
  const activeIndexRef = useRef(activeIndex);
  activeIndexRef.current = activeIndex;
  const supported = highlightApiSupported();
  const units = virtual?.units;

  const clearHighlights = useCallback(() => {
    if (!supported) return;
    CSS.highlights.delete(HIGHLIGHT_NAME);
    CSS.highlights.delete(ACTIVE_HIGHLIGHT_NAME);
  }, [supported]);

  const cancelReveal = useCallback(() => {
    cancelRevealRef.current?.();
    cancelRevealRef.current = null;
  }, []);

  // Paint every match the DOM holds: the static ones, and those inside the
  // units currently mounted. A unit's DOM is the truth for where its matches
  // are; its data text only counts them.
  const paint = useCallback((needle: string) => {
    const mounted = new Map<number, Range[]>();
    const root = virtualRef.current?.root();
    const all: Range[] = [];
    for (const match of matchesRef.current) if ("range" in match) all.push(match.range);
    if (root && needle) {
      const indexByKey = new Map<string, number>();
      unitsRef.current.forEach((unit, index) => indexByKey.set(unit.key, index));
      for (const el of root.querySelectorAll<HTMLElement>(`[${FIND_UNIT_ATTRIBUTE}]`)) {
        const index = indexByKey.get(el.getAttribute(FIND_UNIT_ATTRIBUTE) ?? "");
        if (index === undefined) continue;
        const ranges = collectTextMatches(el, needle).map(toRange);
        if (ranges.length === 0) continue;
        mounted.set(index, ranges);
        all.push(...ranges);
      }
    }
    mountedRef.current = mounted;
    if (!supported) return;
    if (all.length > 0) CSS.highlights.set(HIGHLIGHT_NAME, new Highlight(...all));
    else CSS.highlights.delete(HIGHLIGHT_NAME);
  }, [supported]);

  // Paint the active match on top of the all-matches tint (higher priority)
  // and, when asked, bring it into view — mounting its comment first when it
  // is not in the DOM. Called both when the user navigates and after a
  // recompute, so the active tint always tracks live ranges.
  const activate = useCallback(
    (index: number, scroll: boolean) => {
      const match = index >= 0 ? matchesRef.current[index] : undefined;
      let range: Range | undefined;
      let unitElement: HTMLElement | null = null;
      if (match && "range" in match) range = match.range;
      else if (match) {
        const ranges = mountedRef.current.get(match.unit);
        // Data and DOM can disagree on a count (code highlighting splits text
        // nodes); land on the nearest occurrence the DOM has.
        range = ranges?.[Math.min(match.occurrence, ranges.length - 1)];
        const key = unitsRef.current[match.unit]?.key;
        unitElement = key === undefined ? null
          : virtualRef.current?.root()?.querySelector<HTMLElement>(`[${FIND_UNIT_ATTRIBUTE}="${key}"]`) ?? null;
      }
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
      if (!scroll || !match) return;
      cancelReveal();
      if ("range" in match || unitElement) {
        scrollRectIntoView(containerRef.current, range ? rangeRect(range) : unitElement?.getBoundingClientRect());
        return;
      }
      const unit = unitsRef.current[match.unit];
      const reveal = virtualRef.current?.reveal;
      if (!unit || !reveal) return;
      const pending = { cancel: () => {}, settled: false };
      pending.cancel = reveal(unit, () => {
        pending.settled = true;
        if (cancelRevealRef.current === pending.cancel) cancelRevealRef.current = null;
        if (activeIndexRef.current !== index) return;
        recomputeRef.current({ resetActive: false });
        const ranges = mountedRef.current.get(match.unit);
        const landed = ranges?.[Math.min(match.occurrence, ranges.length - 1)];
        if (landed) scrollRectIntoView(containerRef.current, rangeRect(landed));
      });
      if (!pending.settled) cancelRevealRef.current = pending.cancel;
    },
    [supported, cancelReveal],
  );

  // Rebuild the match set: the DOM outside the virtual region, and the units'
  // text in its place. `resetActive` starts navigation over at the first
  // match (query changed); otherwise the active index is preserved as content
  // shifts underneath (streaming comment, re-highlight, a comment mounting).
  const recompute = useCallback(
    ({ resetActive }: { resetActive: boolean }) => {
      const root = containerRef.current;
      const needle = query.trim().length === 0 ? "" : query.toLowerCase();
      if (!open || !root || !needle) {
        matchesRef.current = [];
        mountedRef.current = new Map();
        clearHighlights();
        setMatchCount(0);
        setActiveIndex(-1);
        return;
      }

      const candidate = virtualRef.current?.root() ?? null;
      const virtualRoot = candidate && root.contains(candidate) ? candidate : null;
      const before: FindMatch[] = [];
      const after: FindMatch[] = [];
      for (const match of collectTextMatches(root, needle, virtualRoot)) {
        const follows = virtualRoot && virtualRoot.compareDocumentPosition(match.node) & Node.DOCUMENT_POSITION_FOLLOWING;
        (follows ? after : before).push({ range: toRange(match) });
      }
      const inside: FindMatch[] = [];
      unitsRef.current = virtualRoot ? virtualRef.current?.units() ?? [] : [];
      unitsRef.current.forEach((unit, index) => {
        const count = countOccurrences(unit.text, needle);
        for (let occurrence = 0; occurrence < count; occurrence++) inside.push({ unit: index, occurrence });
      });
      matchesRef.current = [...before, ...inside, ...after];
      paint(needle);

      const total = matchesRef.current.length;
      setMatchCount(total);
      if (total === 0) {
        cancelReveal();
        setActiveIndex(-1);
        if (supported) CSS.highlights.delete(ACTIVE_HIGHLIGHT_NAME);
        return;
      }
      if (resetActive) {
        // The activation effect scrolls to it.
        setActiveIndex(0);
        setRestart((n) => n + 1);
        return;
      }
      const prev = activeIndexRef.current;
      const nextActive = prev < 0 ? 0 : Math.min(prev, total - 1);
      setActiveIndex(nextActive);
      // Never yank the view while content mutates underneath a fixed match.
      activate(nextActive, false);
    },
    [open, query, supported, clearHighlights, cancelReveal, paint, activate],
  );

  const recomputeRef = useRef(recompute);
  recomputeRef.current = recompute;

  // Recompute when the query changes or the bar opens/closes → restart at the
  // first match. Deferred a frame so a commit in flight has landed before we
  // walk the DOM.
  useEffect(() => {
    const raf = requestAnimationFrame(() => recomputeRef.current({ resetActive: true }));
    return () => cancelAnimationFrame(raf);
  }, [open, query, container]);

  // Recompute when searchable content changes, keeping the active match.
  useEffect(() => {
    if (!open) return;
    const raf = requestAnimationFrame(() => recomputeRef.current({ resetActive: false }));
    return () => cancelAnimationFrame(raf);
  }, [contentKey, units, open]);

  // Async DOM churn (markdown/code highlight settling, streamed replies, rows
  // mounting as the timeline scrolls) invalidates the ranges; re-derive them,
  // coalescing bursts into one frame. Runs regardless of highlight support so
  // fallback counting/navigation stay in sync with the live DOM.
  useEffect(() => {
    if (!open || !container) return;
    let raf = 0;
    const observer = new MutationObserver(() => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => recomputeRef.current({ resetActive: false }));
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

  // Move the active tint and scroll when the user steps between matches or a
  // new query restarts at the first one. (Recompute handles the active tint
  // for content-driven changes.)
  useEffect(() => {
    if (!open) return;
    activate(activeIndex, true);
  }, [activeIndex, restart, open, activate]);

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

  // Drop highlights and any pending landing on close and unmount, so a stale
  // tint can't linger after navigation.
  useEffect(() => {
    if (open) return;
    cancelReveal();
  }, [open, cancelReveal]);
  useEffect(() => () => {
    cancelReveal();
    clearHighlights();
  }, [cancelReveal, clearHighlights]);

  const setQuery = useCallback((value: string) => setQueryState(value), []);
  const openFind = useCallback(() => setOpen(true), []);
  const closeFind = useCallback(() => setOpen(false), []);

  const goNext = useCallback(() => {
    setActiveIndex((prev) => {
      const n = matchesRef.current.length;
      if (n === 0) return -1;
      return prev < 0 ? 0 : (prev + 1) % n;
    });
  }, []);

  const goPrev = useCallback(() => {
    setActiveIndex((prev) => {
      const n = matchesRef.current.length;
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
