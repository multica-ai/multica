// CEREBRO-PATCH(channel-message-search): FIR-407 — in-conversation message
// search for channels and DMs. Net-new feature living entirely in the cerebro
// zone; the upstream channel view only gains a `data-message-id` anchor plus a
// few marked mount points.
//
// Design: the channel timeline is already fully loaded client-side (upstream
// caps it at 2000 entries, no pagination), so search runs over the in-memory
// message stream — no backend round-trip. Matching messages are highlighted in
// place, non-matching ones dimmed, and ▲▼ navigation scrolls the current match
// into view. The in-text highlight uses the CSS Custom Highlight API so it
// never mutates the rich-text DOM that ReadonlyContent renders.
"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";
import { ChevronDown, ChevronUp, Search, X } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import type { TimelineEntry } from "@multica/core/types";

const HIGHLIGHT_NAME = "cerebro-msg-search";
const HIGHLIGHT_CURRENT_NAME = "cerebro-msg-search-current";
const STYLE_ELEMENT_ID = "cerebro-channel-search-style";

// ---------------------------------------------------------------------------
// Pure matching logic — exported for tests.
// ---------------------------------------------------------------------------

/**
 * Return the IDs of the messages whose content contains `query`
 * (case-insensitive substring), in the order they appear in `topLevel`
 * (chronological). Whitespace-only queries match nothing.
 */
export function findMessageMatches(
  topLevel: TimelineEntry[],
  query: string,
): string[] {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const out: string[] = [];
  for (const entry of topLevel) {
    const content = entry.content ?? "";
    if (content.toLowerCase().includes(q)) out.push(entry.id);
  }
  return out;
}

// ---------------------------------------------------------------------------
// Search controller hook.
// ---------------------------------------------------------------------------

export interface ChannelMessageSearch {
  open: boolean;
  query: string;
  setQuery: (q: string) => void;
  toggle: () => void;
  close: () => void;
  /** Total number of matching messages. */
  matchCount: number;
  /** 1-based position of the current match, 0 when there is none. */
  position: number;
  /** Move to the next (newer) match. */
  next: () => void;
  /** Move to the previous (older) match. */
  prev: () => void;
}

export function useChannelMessageSearch(
  topLevel: TimelineEntry[],
  scrollRef: RefObject<HTMLElement | null>,
): ChannelMessageSearch {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [currentId, setCurrentId] = useState<string | null>(null);

  const matchedIds = useMemo(
    () => findMessageMatches(topLevel, query),
    [topLevel, query],
  );

  // When the query changes, jump to the most recent match (Slack-like). Done in
  // an effect so it follows the recomputed `matchedIds`.
  useEffect(() => {
    setCurrentId(matchedIds[matchedIds.length - 1] ?? null);
    // Intentionally keyed on the query only: a new message arriving should not
    // yank the user away from the match they navigated to.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);

  // Keep the current match valid if the underlying list shifts (deletes etc.).
  useEffect(() => {
    if (currentId && !matchedIds.includes(currentId)) {
      setCurrentId(matchedIds[matchedIds.length - 1] ?? null);
    }
  }, [matchedIds, currentId]);

  const currentIndex = currentId ? matchedIds.indexOf(currentId) : -1;

  const step = useCallback(
    (delta: number) => {
      if (matchedIds.length === 0) return;
      const base = currentIndex === -1 ? matchedIds.length - 1 : currentIndex;
      const len = matchedIds.length;
      const nextIndex = (base + delta + len) % len;
      setCurrentId(matchedIds[nextIndex] ?? null);
    },
    [matchedIds, currentIndex],
  );

  const next = useCallback(() => step(1), [step]);
  const prev = useCallback(() => step(-1), [step]);

  const close = useCallback(() => {
    setOpen(false);
    setQuery("");
    setCurrentId(null);
  }, []);

  const toggle = useCallback(() => {
    setOpen((o) => {
      if (o) {
        setQuery("");
        setCurrentId(null);
      }
      return !o;
    });
  }, []);

  // Apply visual state (dim / highlight / scroll) imperatively. The row
  // elements are owned by the upstream renderer, so we drive presentation via a
  // `data-msg-search` attribute (which React never reconciles away) plus the
  // CSS Custom Highlight API for the in-text marks.
  useEffect(() => {
    ensureStyleInjected();
    const container = scrollRef.current;
    if (!container) return;

    const rows = container.querySelectorAll<HTMLElement>("[data-message-id]");
    const active = open && query.trim().length > 0 && matchedIds.length > 0;

    if (!active) {
      clearRowState(rows);
      clearTextHighlights();
      return;
    }

    const matchSet = new Set(matchedIds);
    rows.forEach((row) => {
      const id = row.getAttribute("data-message-id");
      if (id && matchSet.has(id)) {
        row.dataset.msgSearch = id === currentId ? "current" : "match";
      } else {
        row.dataset.msgSearch = "dim";
      }
    });

    applyTextHighlights(rows, matchSet, currentId, query.trim());

    if (currentId) {
      const currentRow = container.querySelector<HTMLElement>(
        `[data-message-id="${cssEscape(currentId)}"]`,
      );
      currentRow?.scrollIntoView({ block: "center", behavior: "smooth" });
    }
  }, [open, query, matchedIds, currentId, scrollRef]);

  // Clear everything when the controller unmounts (e.g. switching channels).
  useEffect(() => {
    return () => {
      const container = scrollRef.current;
      if (container) {
        clearRowState(container.querySelectorAll<HTMLElement>("[data-message-id]"));
      }
      clearTextHighlights();
    };
  }, [scrollRef]);

  return {
    open,
    query,
    setQuery,
    toggle,
    close,
    matchCount: matchedIds.length,
    position: currentIndex === -1 ? 0 : currentIndex + 1,
    next,
    prev,
  };
}

// ---------------------------------------------------------------------------
// Header search-toggle button.
// ---------------------------------------------------------------------------

export interface ChannelMessageSearchButtonProps {
  active: boolean;
  onToggle: () => void;
}

export function ChannelMessageSearchButton({
  active,
  onToggle,
}: ChannelMessageSearchButtonProps) {
  const enabled = useFeatureFlag("cerebro_channel_message_search");
  if (!enabled) return null;
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            onClick={onToggle}
            aria-label="Søg i samtale"
            aria-pressed={active}
            className={cn(
              "inline-flex size-7 items-center justify-center rounded border text-muted-foreground hover:bg-accent hover:text-foreground",
              active && "bg-accent text-foreground",
            )}
          />
        }
      >
        <Search className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent side="bottom">Søg i samtale</TooltipContent>
    </Tooltip>
  );
}

// ---------------------------------------------------------------------------
// Search bar shown under the channel header.
// ---------------------------------------------------------------------------

export interface ChannelMessageSearchBarProps {
  search: ChannelMessageSearch;
  /** Optional placeholder, e.g. "Søg i #firtal-portal…". */
  placeholder?: string;
}

export function ChannelMessageSearchBar({
  search,
  placeholder,
}: ChannelMessageSearchBarProps) {
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const { query, setQuery, matchCount, position, next, prev, close } = search;
  const trimmed = query.trim();

  const countLabel = !trimmed
    ? ""
    : matchCount === 0
      ? "Ingen resultater"
      : `${position} af ${matchCount}`;

  return (
    <div className="flex shrink-0 items-center gap-2 border-b bg-muted/40 px-4 py-2">
      <div className="flex flex-1 items-center gap-2 rounded-md border border-primary bg-background px-3 py-1.5">
        <Search className="size-3.5 shrink-0 text-primary" />
        <Input
          ref={inputRef}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              close();
            } else if (e.key === "Enter") {
              e.preventDefault();
              if (e.shiftKey) prev();
              else next();
            }
          }}
          placeholder={placeholder ?? "Søg i samtale…"}
          aria-label="Søg i samtale"
          className="h-5 border-0 bg-transparent p-0 text-sm shadow-none focus-visible:ring-0"
        />
      </div>
      {countLabel && (
        <span className="whitespace-nowrap text-xs text-muted-foreground tabular-nums">
          {countLabel}
        </span>
      )}
      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={prev}
          disabled={matchCount === 0}
          aria-label="Forrige resultat"
          className="inline-flex size-7 items-center justify-center rounded border text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-40"
        >
          <ChevronUp className="size-3.5" />
        </button>
        <button
          type="button"
          onClick={next}
          disabled={matchCount === 0}
          aria-label="Næste resultat"
          className="inline-flex size-7 items-center justify-center rounded border text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-40"
        >
          <ChevronDown className="size-3.5" />
        </button>
      </div>
      <button
        type="button"
        onClick={close}
        aria-label="Luk søgning"
        className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
      >
        <X className="size-3.5" />
        Luk
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// DOM / CSS helpers.
// ---------------------------------------------------------------------------

function clearRowState(rows: NodeListOf<HTMLElement>) {
  rows.forEach((row) => {
    delete row.dataset.msgSearch;
  });
}

interface CSSHighlightRegistry {
  set(name: string, highlight: unknown): void;
  delete(name: string): void;
}

function getHighlightRegistry(): CSSHighlightRegistry | null {
  const reg = (globalThis as unknown as { CSS?: { highlights?: CSSHighlightRegistry } })
    .CSS?.highlights;
  return reg ?? null;
}

function makeHighlight(ranges: Range[]): unknown | null {
  const Ctor = (globalThis as unknown as { Highlight?: new (...r: Range[]) => unknown })
    .Highlight;
  if (!Ctor || ranges.length === 0) return null;
  return new Ctor(...ranges);
}

function clearTextHighlights() {
  const reg = getHighlightRegistry();
  if (!reg) return;
  reg.delete(HIGHLIGHT_NAME);
  reg.delete(HIGHLIGHT_CURRENT_NAME);
}

function applyTextHighlights(
  rows: NodeListOf<HTMLElement>,
  matchSet: Set<string>,
  currentId: string | null,
  query: string,
) {
  const reg = getHighlightRegistry();
  if (!reg) return; // Graceful degradation: row-level highlight still applies.

  const matchRanges: Range[] = [];
  const currentRanges: Range[] = [];
  const needle = query.toLowerCase();

  rows.forEach((row) => {
    const id = row.getAttribute("data-message-id");
    if (!id || !matchSet.has(id)) return;
    const target = id === currentId ? currentRanges : matchRanges;
    collectRanges(row, needle, target);
  });

  const matchHighlight = makeHighlight(matchRanges);
  const currentHighlight = makeHighlight(currentRanges);
  if (matchHighlight) reg.set(HIGHLIGHT_NAME, matchHighlight);
  else reg.delete(HIGHLIGHT_NAME);
  if (currentHighlight) reg.set(HIGHLIGHT_CURRENT_NAME, currentHighlight);
  else reg.delete(HIGHLIGHT_CURRENT_NAME);
}

function collectRanges(root: HTMLElement, needle: string, out: Range[]) {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  while (node) {
    const value = node.nodeValue ?? "";
    const haystack = value.toLowerCase();
    let from = 0;
    let idx = haystack.indexOf(needle, from);
    while (idx !== -1) {
      const range = document.createRange();
      range.setStart(node, idx);
      range.setEnd(node, idx + needle.length);
      out.push(range);
      from = idx + needle.length;
      idx = haystack.indexOf(needle, from);
    }
    node = walker.nextNode();
  }
}

function ensureStyleInjected() {
  if (typeof document === "undefined") return;
  if (document.getElementById(STYLE_ELEMENT_ID)) return;
  const style = document.createElement("style");
  style.id = STYLE_ELEMENT_ID;
  // Highlight colours come straight from the approved mockup (FIR-407). There
  // is no semantic design token for "search highlight", so the amber/yellow
  // pair is intentional and shared across light/dark themes.
  style.textContent = `
[data-msg-search="dim"] { opacity: 0.35; transition: opacity 0.15s ease; }
[data-msg-search="match"],
[data-msg-search="current"] {
  background: rgba(245, 158, 11, 0.08);
  box-shadow: inset 3px 0 0 #f59e0b;
  border-radius: 0 6px 6px 0;
}
[data-msg-search="current"] { background: rgba(245, 158, 11, 0.16); }
::highlight(${HIGHLIGHT_NAME}) { background-color: #fef08a; color: #92400e; }
::highlight(${HIGHLIGHT_CURRENT_NAME}) { background-color: #f59e0b; color: #ffffff; }
`;
  document.head.appendChild(style);
}

function cssEscape(value: string): string {
  const fn = (globalThis as unknown as { CSS?: { escape?: (s: string) => string } })
    .CSS?.escape;
  return fn ? fn(value) : value.replace(/["\\]/g, "\\$&");
}
