"use client";

import { useCallback, useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { CheckCircle2, MessageSquare, MessagesSquare, Search } from "lucide-react";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { createShortcutChord, useShortcut } from "@multica/core/shortcuts";
import { preprocessMentionShortcodes } from "@multica/core/markdown";
import { isImeComposing } from "@multica/core/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { pickerNavigationDirection } from "../../common/picker-keys";
import { ShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { commentPreview } from "./thread-minimap";

// ---------------------------------------------------------------------------
// ThreadNavPanel — header entry point for jumping between comment threads
// ---------------------------------------------------------------------------
//
// The right-edge rail (ThreadMinimap) is a good position indicator and a bad
// finder: a tick carries no text, so locating a specific thread costs one
// hover per candidate, and once threads outgrow the rail's height the ticks
// compress to their 5px floor and stop being countable at all. A list fixes
// the cost model — 8-10 titles readable in one glance instead of one at a
// time — and search/filter make 24 threads and 240 threads cost the same
// (MUL-5755).
//
// The panel deliberately does NOT mark which threads are currently on screen.
// "On screen" is a set, not a point — a tall viewport holds several threads at
// once, so the marker landed on two or three rows simultaneously and read as a
// broken multi-select rather than as a position. Absolute position is the
// rail's job, where a column of ticks can show a span honestly; the panel's job
// is finding. Hovering a row still lights that thread's tick on the rail, which
// links the two without duplicating the rail's answer.
//
// Open model — hover previews, press pins:
//
//   hover 200ms  -> open, unpinned. Pointer-out closes it. No focus is moved,
//                   so a half-typed comment never loses the caret.
//   press        -> pinned. Search takes focus, arrow keys drive the list, and
//                   only Escape / outside press / a second press closes it.
//   focus or scroll inside the panel -> pinned.
//
// Pure hover would have been the wrong trigger for a surface you must be able
// to scroll, type into, and arrow through: all three need the pointer free to
// leave the button. Pinning on *intent* (press, focus, scroll) rather than on
// mere pointer-entry keeps a pass-through glide from leaving the panel stuck
// open.

/** Below this the header button is noise — an issue with no discussion. */
const MIN_THREADS = 1;

/** Hover intent delay before the preview opens. */
const HOVER_OPEN_DELAY_MS = 200;
/** Grace period on leave — long enough to travel from the button into the panel. */
const HOVER_CLOSE_DELAY_MS = 200;

const DAY_MS = 24 * 60 * 60 * 1000;

// Footer hint keys. Built as real chords so ShortcutKeycaps renders each one
// with the platform's own glyph and keycap chrome.
const KEY_UP = createShortcutChord("Up");
const KEY_DOWN = createShortcutChord("Down");
const KEY_ENTER = createShortcutChord("Enter");
const KEY_ESCAPE = createShortcutChord("Escape");

export type ThreadNavFilter = "all" | "unresolved" | "resolved" | "mine";

export interface ThreadNavThread {
  /** Root comment id — also the `comment-${id}` DOM anchor of the rendered row. */
  id: string;
  /** The thread's root comment entry (preview text, author, timestamp). */
  entry: TimelineEntry;
  /** Derived by the caller with `deriveThreadResolution` — covers reply resolutions too. */
  resolved: boolean;
  /** Replies under the root, excluding the root itself. */
  replyCount: number;
  /** The current user authored, replied to, or was @mentioned in this thread. */
  involvesMe: boolean;
}

export type ThreadDayGroup = "today" | "yesterday" | "earlier";

/**
 * Bucket a thread by the calendar day of its root comment, in the reader's
 * local time zone. Only three buckets: past "yesterday" a per-day header
 * would produce more headers than rows on a long-running issue, which is
 * noise rather than structure — the timestamp on each row already carries
 * the exact date once the reader is in "earlier".
 */
export function threadDayGroup(createdAt: string, nowMs: number): ThreadDayGroup {
  const ts = Date.parse(createdAt);
  if (Number.isNaN(ts)) return "earlier";
  const startOfToday = new Date(nowMs).setHours(0, 0, 0, 0);
  if (ts >= startOfToday) return "today";
  if (ts >= startOfToday - DAY_MS) return "yesterday";
  return "earlier";
}

/** A thread plus everything the row and the filters need, computed once. */
interface PreparedThread {
  thread: ThreadNavThread;
  title: string;
  excerpt: string;
  authorName: string;
  group: ThreadDayGroup;
  /** Lowercased `title + excerpt + author`, the haystack the query runs against. */
  haystack: string;
}

export function matchesFilter(thread: ThreadNavThread, filter: ThreadNavFilter): boolean {
  switch (filter) {
    case "unresolved":
      return !thread.resolved;
    case "resolved":
      return thread.resolved;
    case "mine":
      return thread.involvesMe;
    case "all":
      return true;
    default:
      // Server-driven values never reach this union, but an exhaustive
      // default keeps a future filter from silently hiding every thread.
      return true;
  }
}

/**
 * Whether `content` @mentions `userId`.
 *
 * Current mentions serialize as markdown links to `mention://member/<uuid>`,
 * but the legacy `[@ id="..." label="..."]` shortcode form is still sitting in
 * the database — `preprocessMarkdown` migrates it *on read*, not before
 * storage, and the timeline hands us raw content. Matching only the link form
 * silently dropped every thread whose sole mention of the reader was written in
 * the old format. `preprocessMentionShortcodes` returns its input untouched
 * when there is no `[@ ` in it, so the normal path costs one substring scan.
 */
export function mentionsUser(content: string | undefined, userId: string): boolean {
  if (!content || !userId) return false;
  return preprocessMentionShortcodes(content).includes(`mention://member/${userId}`);
}

/**
 * Split `text` on every case-insensitive occurrence of `query`, tinting the
 * matches. Without this a filtered list asks the reader to re-find the term
 * they just typed — the row that matched on its excerpt looks identical to the
 * one that matched on its title. Uses the same `--find-match` tint as the
 * in-page find bar so "this is your search term" reads the same everywhere.
 */
export function highlightMatches(text: string, query: string): ReactNode {
  const needle = query.trim();
  if (needle === "") return text;
  const lowerText = text.toLowerCase();
  const lowerNeedle = needle.toLowerCase();
  const out: ReactNode[] = [];
  let from = 0;
  let at = lowerText.indexOf(lowerNeedle);
  while (at !== -1) {
    if (at > from) out.push(text.slice(from, at));
    out.push(
      <mark
        key={at}
        className="rounded-[3px] bg-[var(--find-match)] px-px text-[var(--find-match-foreground)]"
      >
        {text.slice(at, at + needle.length)}
      </mark>,
    );
    from = at + needle.length;
    at = lowerText.indexOf(lowerNeedle, from);
  }
  if (from < text.length) out.push(text.slice(from));
  return out;
}

function ThreadRow({
  prepared,
  isActive,
  optionId,
  query,
  onJump,
  onHover,
}: {
  prepared: PreparedThread;
  /** Keyboard cursor is on this row. */
  isActive: boolean;
  /** Stable DOM id so the search field can point at this row. */
  optionId: string;
  /** Active search term, tinted inside the title and excerpt. */
  query: string;
  onJump: () => void;
  onHover: (threadId: string | null) => void;
}) {
  const { t } = useT("issues");
  const { thread, title, excerpt, authorName } = prepared;
  return (
    <button
      type="button"
      // Not a tab stop, and role=option: the search field keeps DOM focus and
      // names this row through aria-activedescendant. Leaving the rows
      // focusable produced two cursors — Tab to row 3, ArrowDown highlights
      // row 2, Enter clicks row 3 — because DOM focus and the highlight were
      // separate pieces of state with nothing keeping them in step.
      tabIndex={-1}
      role="option"
      id={optionId}
      aria-selected={isActive}
      data-thread-id={thread.id}
      data-active={isActive || undefined}
      onClick={onJump}
      onPointerEnter={() => onHover(thread.id)}
      onPointerLeave={() => onHover(null)}
      className={cn(
        "relative flex w-full items-start gap-2.5 rounded-md px-2 py-2 text-left outline-none transition-colors",
        "hover:bg-surface-hover data-active:bg-surface-hover",
      )}
    >
      <ActorAvatar
        actorType={thread.entry.actor_type}
        actorId={thread.entry.actor_id}
        size="sm"
        profileLink={false}
        className="mt-0.5 shrink-0"
      />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex items-baseline gap-1.5">
          <span
            className={cn(
              "min-w-0 flex-1 truncate text-body",
              thread.resolved ? "text-muted-foreground" : "font-medium text-foreground",
            )}
          >
            {highlightMatches(title, query)}
          </span>
          <span className="shrink-0 text-micro tabular-nums text-faint-foreground">
            {formatStamp(prepared.thread.entry.created_at, prepared.group)}
          </span>
        </span>
        <span className="mt-1 flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          <span className="shrink-0">{authorName}</span>
          {thread.replyCount > 0 && (
            <span className="flex shrink-0 items-center gap-0.5 tabular-nums text-faint-foreground">
              <MessageSquare className="h-3 w-3" />
              {thread.replyCount}
            </span>
          )}
          {thread.resolved && (
            <span className="flex shrink-0 items-center gap-0.5 text-success">
              <CheckCircle2 className="h-3 w-3" />
              {t(($) => $.comment.resolve.thread_resolved_badge)}
            </span>
          )}
          {excerpt && (
            <span className="min-w-0 truncate text-faint-foreground">
              {highlightMatches(excerpt, query)}
            </span>
          )}
        </span>
      </span>
    </button>
  );
}

/**
 * Wall-clock under a today/yesterday header, calendar date under "earlier" —
 * the header already names the day in the first two cases, so repeating it per
 * row would be redundant where it is known and missing where it is not.
 *
 * Driven by the row's own group rather than a second "less than 48 hours ago"
 * test. Those two boundaries disagree for most of the day: an entry from
 * 23:41 the day before yesterday is under 48 hours old but groups under
 * "earlier", and it rendered as a bare `11:41 PM` with nothing saying which
 * day. One boundary, decided once, cannot drift from itself.
 */
export function formatStamp(createdAt: string, group: ThreadDayGroup): string {
  const ts = Date.parse(createdAt);
  if (Number.isNaN(ts)) return "";
  const d = new Date(ts);
  return group === "earlier"
    ? d.toLocaleDateString(undefined, { month: "short", day: "numeric" })
    : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

interface ThreadNavPanelProps {
  threads: ThreadNavThread[];
  onJump: (threadId: string) => void;
  /** Lights the matching tick on the right-edge rail. */
  onHoverThread: (threadId: string | null) => void;
  /** Controlled open state, so the global shortcut can pin the panel open. */
  open: boolean;
  onOpenChange: (open: boolean, pinned: boolean) => void;
  /** True when `open` was reached by a press / shortcut rather than by hover. */
  pinned: boolean;
}

export function ThreadNavPanel({
  threads,
  onJump,
  onHoverThread,
  open,
  onOpenChange,
  pinned,
}: ThreadNavPanelProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  // Subscribed, not snapshotted: `getShortcut` reads the store once with no
  // subscription, so a rebind in Settings left this tooltip showing the old
  // key while the actual keydown handler already used the new one.
  const openShortcutChord = useShortcut("openThreadNav");
  const listId = useId();
  const optionId = useCallback((threadId: string) => `${listId}-${threadId}`, [listId]);

  const [tooltipOpen, setTooltipOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<ThreadNavFilter>("all");
  const [activeIndex, setActiveIndex] = useState(0);
  const searchRef = useRef<HTMLInputElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  // Previews are cached by content so an unrelated timeline update (a reaction,
  // a reply in another thread) doesn't re-flatten every root comment.
  const previewCacheRef = useRef(
    new Map<string, { content: string | undefined; preview: { title: string; body: string } }>(),
  );
  const prepared = useMemo<PreparedThread[]>(() => {
    const nowMs = Date.now();
    const nextCache = new Map<
      string,
      { content: string | undefined; preview: { title: string; body: string } }
    >();
    const rows = threads.map((thread) => {
      const cached = previewCacheRef.current.get(thread.id);
      const preview =
        cached && cached.content === thread.entry.content
          ? cached.preview
          : commentPreview(thread.entry.content ?? "");
      nextCache.set(thread.id, { content: thread.entry.content, preview });
      const authorName = getActorName(thread.entry.actor_type, thread.entry.actor_id);
      const title = preview.title || authorName;
      return {
        thread,
        title,
        excerpt: preview.body,
        authorName,
        group: threadDayGroup(thread.entry.created_at, nowMs),
        haystack: `${title}\n${preview.body}\n${authorName}`.toLowerCase(),
      };
    });
    previewCacheRef.current = nextCache;
    return rows;
  }, [threads, getActorName]);

  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return prepared.filter(
      (row) =>
        matchesFilter(row.thread, filter) &&
        (needle === "" || row.haystack.includes(needle)),
    );
  }, [prepared, filter, query]);

  const counts = useMemo(
    () => ({
      all: threads.length,
      unresolved: threads.filter((th) => !th.resolved).length,
      resolved: threads.filter((th) => th.resolved).length,
      mine: threads.filter((th) => th.involvesMe).length,
    }),
    [threads],
  );

  // Each opening starts clean: no carried-over query, filter, or cursor.
  const openRef = useRef(open);
  useEffect(() => {
    if (open && !openRef.current) {
      setQuery("");
      setFilter("all");
      setActiveIndex(0);
    }
    openRef.current = open;
  }, [open]);

  // Filtering shrinks the list under the cursor; clamp instead of leaving it
  // pointing past the end, where ↵ would do nothing.
  useEffect(() => {
    setActiveIndex((prev) => (prev >= rows.length ? Math.max(0, rows.length - 1) : prev));
  }, [rows.length]);

  // Keep the keyboard cursor in view — it can be driven far past the visible
  // slice by held arrow keys.
  useEffect(() => {
    if (!open) return;
    const row = rows[activeIndex];
    if (!row) return;
    listRef.current
      ?.querySelector<HTMLElement>(`[data-thread-id="${CSS.escape(row.thread.id)}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [activeIndex, open, rows]);

  const pin = useCallback(() => onOpenChange(true, true), [onOpenChange]);

  // Focus follows `pinned`, not the press handler, so the global shortcut —
  // which sets the pinned state directly — lands the caret in search too.
  // Deferred a frame because Base UI runs its own focus pass on open and would
  // otherwise take the caret back.
  useEffect(() => {
    if (!open || !pinned) return;
    const raf = requestAnimationFrame(() => searchRef.current?.focus());
    return () => cancelAnimationFrame(raf);
  }, [open, pinned]);

  const close = useCallback(() => {
    onHoverThread(null);
    onOpenChange(false, false);
  }, [onHoverThread, onOpenChange]);

  const jump = useCallback(
    (threadId: string) => {
      onHoverThread(null);
      onJump(threadId);
      onOpenChange(false, false);
    },
    [onHoverThread, onJump, onOpenChange],
  );

  const handleOpenChange = useCallback(
    (next: boolean, details: { reason?: string }) => {
      const reason = details?.reason;
      if (next) {
        if (reason === "trigger-press") pin();
        else onOpenChange(true, false);
        return;
      }
      // Pressing the trigger of a hover-preview means "keep this, I want to
      // work in it" — not "close". Only a press on an already-pinned panel
      // closes it.
      if (reason === "trigger-press" && !pinned) {
        pin();
        return;
      }
      // A pinned panel outlives the pointer: the user has to be able to reach
      // the scrollbar and the rows without the panel evaporating behind them.
      if (pinned && reason === "trigger-hover") return;
      close();
    },
    [close, pin, pinned, onOpenChange],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      // An IME commit arrives as Enter (and the aliases as plain letters) while
      // composing. Acting on those would jump away mid-word and close the panel
      // the moment a CJK user picked a candidate — the search field is the one
      // place in this panel people type prose.
      if (isImeComposing(e)) return;
      // The search field owns the list keyboard, arrows included. Letting the
      // arrows fire from anywhere in the popup while Enter fired only from the
      // search field was the worst of both: focus on a filter pill, ArrowDown
      // moved the highlight, and Enter then activated the pill — two cursors,
      // disagreeing.
      if (e.target !== searchRef.current) return;
      // Arrow keys plus the Ctrl+N/J and Ctrl+P/K aliases, from the same policy
      // the editor's pickers use — which in turn mirrors what cmdk's
      // `vimBindings` gives the command bar for free. One list-with-a-highlight
      // should not navigate differently from the next.
      const direction = pickerNavigationDirection(e.nativeEvent);
      if (direction) {
        // preventDefault matters for the letter aliases specifically: focus is
        // in the search field, where Ctrl+K/N/P are readline editing commands
        // that would otherwise mangle the query while moving the cursor.
        e.preventDefault();
        if (rows.length === 0) return;
        const step = direction === "next" ? 1 : -1;
        setActiveIndex((prev) => (prev + step + rows.length) % rows.length);
        return;
      }
      // Enter is gated by the same target check above, which is what keeps the
      // filter pills activatable: `preventDefault` on a button's keydown
      // cancels the click the browser was about to synthesize, so handling
      // Enter for the whole popup made "Resolved" jump instead of filter.
      if (e.key === "Enter") {
        const row = rows[activeIndex];
        if (!row) return;
        e.preventDefault();
        jump(row.thread.id);
      }
      // Escape is Base UI's job — it already routes through onOpenChange and
      // restores focus to the trigger.
      //
      // Tab is deliberately NOT an accept key here, unlike the editor pickers:
      // this is a popover, and Tab has to stay focus navigation so the filter
      // pills remain keyboard-reachable.
    },
    [activeIndex, jump, rows],
  );

  if (threads.length < MIN_THREADS) return null;

  const buttonLabel = t(($) => $.detail.thread_nav.button_label, { count: threads.length });
  const openShortcut = openShortcutChord;

  const filterPills: { id: ThreadNavFilter; label: string; count: number }[] = [
    { id: "all", label: t(($) => $.detail.thread_nav.filter_all), count: counts.all },
    { id: "unresolved", label: t(($) => $.detail.thread_nav.filter_unresolved), count: counts.unresolved },
    { id: "resolved", label: t(($) => $.detail.thread_nav.filter_resolved), count: counts.resolved },
    { id: "mine", label: t(($) => $.detail.thread_nav.filter_mine), count: counts.mine },
  ];

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      {/* Base UI puts the hover config on the Trigger, not the Root (a popover
          may have several triggers). The trigger stays a real button so press
          and keyboard still work for touch and a11y. */}
      {/* Tooltip like every other control in this header, and the only place
          the open shortcut is discoverable — same label + keycaps shape the
          chat launcher uses. Held controlled for its whole life (never
          `undefined`, which would flip it uncontrolled mid-flight) and forced
          shut while the panel is open, so the tip never sits on top of it. */}
      <Tooltip open={tooltipOpen && !open} onOpenChange={setTooltipOpen}>
        <TooltipTrigger
          render={
            <PopoverTrigger
              openOnHover
              delay={HOVER_OPEN_DELAY_MS}
              closeDelay={HOVER_CLOSE_DELAY_MS}
              render={
                <Button
                  variant={open ? "secondary" : "ghost"}
                  size="sm"
                  aria-label={buttonLabel}
                  className={cn("h-7 gap-1.5 px-2", !open && "text-muted-foreground")}
                />
              }
            >
              {/* Explicit `size-4` so the icon matches the neighbouring header
                  action buttons instead of the `sm` button's smaller default. */}
              <MessagesSquare className="size-4" />
              <span className="text-caption font-medium tabular-nums">{threads.length}</span>
            </PopoverTrigger>
          }
        />
        <TooltipContent side="bottom">
          {buttonLabel}
          {openShortcut ? (
            <ShortcutKeycaps shortcut={openShortcut} decorative className="ml-1.5" />
          ) : null}
        </TooltipContent>
      </Tooltip>
      <PopoverContent
        align="end"
        // Step inside the right-edge rail's 32px strip so the two navigators
        // stay readable together: the rail keeps showing absolute position
        // while the panel does the finding. On the alignment axis a positive
        // offset moves the popup toward the edge opposite the aligned one, so
        // with `align="end"` this shifts left, away from the rail.
        alignOffset={24}
        // Hover previews must never steal the caret from a half-written
        // comment; `pin()` focuses the search field explicitly instead.
        initialFocus={false}
        onKeyDown={handleKeyDown}
        // Any deliberate interaction inside the panel upgrades a preview to
        // pinned. Pointer-entry alone deliberately does not: a glide across
        // the panel on the way somewhere else should not leave it stuck open.
        onFocusCapture={pinned ? undefined : pin}
        // Every band insets its content to the same 14px: the search icon and
        // the footer sit at px-3.5, while the pill row and the list reach it as
        // container padding + item padding (6 + 8). At 10 / 18 / 12 the three
        // bands were close enough to read as a mistake and far enough apart to
        // see, which is what made the panel feel unsettled rather than dense.
        // 400px rather than 380 so the wider inset costs no excerpt width.
        className="flex w-[400px] flex-col gap-0 p-0"
      >
        {/* Search and filters are one header block, so the rule that used to
            sit between them is gone: at three dividers a 380px popover read as
            stacked bands rather than as one surface, and the filter row was the
            thinnest band of the three. */}
        <div className="flex h-11 shrink-0 items-center gap-2.5 px-3.5">
          <Search className="size-4 shrink-0 text-faint-foreground" />
          <input
            ref={searchRef}
            role="combobox"
            aria-expanded
            aria-controls={listId}
            aria-activedescendant={rows[activeIndex] ? optionId(rows[activeIndex]!.thread.id) : undefined}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActiveIndex(0);
            }}
            placeholder={t(($) => $.detail.thread_nav.search_placeholder)}
            aria-label={t(($) => $.detail.thread_nav.search_placeholder)}
            className="min-w-0 flex-1 bg-transparent text-body text-foreground outline-none placeholder:text-faint-foreground"
          />
          {query.trim() !== "" && (
            <span className="shrink-0 text-caption tabular-nums text-faint-foreground">
              {t(($) => $.detail.thread_nav.match_count, { count: rows.length })}
            </span>
          )}
        </div>

        {/* This divider stays: it is the top edge of a scroll region, and
            without it the list slides under the pills with nothing marking the
            boundary. */}
        <div className="flex shrink-0 items-center gap-1 border-b border-border px-1.5 py-2">
          {filterPills.map((pill) => (
            <button
              key={pill.id}
              type="button"
              onClick={() => {
                setFilter(pill.id);
                setActiveIndex(0);
              }}
              data-active={filter === pill.id || undefined}
              className={cn(
                "flex h-7 items-center gap-1 rounded-full px-2 text-caption text-muted-foreground transition-colors",
                "hover:bg-surface-hover",
                "data-active:bg-surface-selected data-active:font-medium data-active:text-foreground data-active:hover:bg-surface-selected",
              )}
            >
              {pill.label}
              <span className="tabular-nums text-faint-foreground">{pill.count}</span>
            </button>
          ))}
        </div>

        <div
          ref={listRef}
          id={listId}
          role="listbox"
          aria-label={t(($) => $.detail.thread_nav.search_placeholder)}
          // Scrolling is an intent signal: the reader is working the list, so
          // the panel should survive the pointer leaving afterwards.
          onScroll={pinned ? undefined : pin}
          className="max-h-[26rem] min-h-0 flex-1 overflow-y-auto p-1.5"
        >
          {rows.length === 0 ? (
            <p className="px-2 py-8 text-center text-caption text-muted-foreground">
              {t(($) => $.detail.thread_nav.empty)}
            </p>
          ) : (
            rows.map((row, i) => {
              const prev = rows[i - 1];
              const showHeader = !prev || prev.group !== row.group;
              return (
                <div key={row.thread.id}>
                  {/* Asymmetric on purpose — a day label belongs to the group
                      under it, so it sits closer to its own first row than to
                      the previous group's last one. */}
                  {showHeader && (
                    <p className="px-2 pb-1 pt-3 text-micro font-medium text-faint-foreground">
                      {t(($) => $.detail.thread_nav.group[row.group])}
                    </p>
                  )}
                  <ThreadRow
                    prepared={row}
                    isActive={i === activeIndex}
                    optionId={optionId(row.thread.id)}
                    query={query}
                    onJump={() => jump(row.thread.id)}
                    onHover={onHoverThread}
                  />
                </div>
              );
            })
          )}
        </div>

        {/* Keycaps come from the shared ShortcutKeycaps, so these read as the
            same keys the sidebar, command palette, and confirm dialogs draw —
            real keycap chrome and the platform's own glyphs, not arrows typed
            into a translation string. */}
        <div className="flex h-9 shrink-0 items-center gap-4 border-t border-border px-3.5 text-micro text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="flex items-center gap-0.5">
              <ShortcutKeycaps shortcut={KEY_UP} />
              <ShortcutKeycaps shortcut={KEY_DOWN} />
            </span>
            {t(($) => $.detail.thread_nav.hint_select)}
          </span>
          <span className="flex items-center gap-1.5">
            <ShortcutKeycaps shortcut={KEY_ENTER} />
            {t(($) => $.detail.thread_nav.hint_jump)}
          </span>
          <span className="flex items-center gap-1.5">
            <ShortcutKeycaps shortcut={KEY_ESCAPE} />
            {t(($) => $.detail.thread_nav.hint_close)}
          </span>
        </div>
      </PopoverContent>
    </Popover>
  );
}
