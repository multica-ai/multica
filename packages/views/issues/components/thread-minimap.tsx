import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CheckCircle2 } from "lucide-react";
import type { TimelineEntry } from "@multica/core/types";
import { isDeletedComment } from "@multica/core/issues/comment-deletion";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useT, useTimeAgo } from "../../i18n";

// ---------------------------------------------------------------------------
// ThreadMinimap — quick-jump rail with a complete comment outline.
// Every thread is a long tick and every reply a short tick beneath it, so the
// rail doubles as a map of where the conversation runs long. Hovering or
// focusing any tick opens one stationary, scrollable outline of every thread
// with its replies listed under it. Rows jump to the same timeline anchors as
// the ticks, including replies inside collapsed or resolved threads.

/** Minimum number of threads before the rail is worth its pixels. */
const MIN_THREADS = 2;

/**
 * Most ticks the rail draws before it drops the reply ticks and keeps one per
 * thread. Past this, ticks compressed to their minimum pitch stop being
 * individually targetable; the outline still lists every reply.
 */
export const MAX_RAIL_TICKS = 80;

/** Intent delay before the card first appears; gliding afterwards is instant. */
const PREVIEW_OPEN_DELAY_MS = 150;
/** Grace period on leave — long enough to travel from rail onto the card. */
const PREVIEW_CLOSE_DELAY_MS = 150;

// ---------------------------------------------------------------------------
// Hover wave — Dock-style proximity magnification
// ---------------------------------------------------------------------------
//
// While the pointer travels along the rail, every tick scales with a cosine
// falloff of its distance to the cursor, so the hovered tick peaks and its
// neighbours taper off like a wave. Driven per-pointermove with direct style
// writes (no React re-render), batched read-then-write inside one rAF, on the
// compositor-friendly native `scale` property; the 100ms ease-out transition
// on the tick smooths between pointer samples and settles the collapse on
// leave. Only the hovered tick darkens — neighbours grow but keep their color.

/** Distance (px) at which a tick stops feeling the wave — ~4 tick pitches. */
const WAVE_RADIUS_PX = 56;
/** Peak horizontal scale of the hovered tick (12px base → ~20px). */
const WAVE_MAX_SCALE = 1.7;

/**
 * Horizontal scale for a tick whose center is `distancePx` from the pointer.
 * Cosine-squared bell: smooth at the peak and at the radius edge (no kinks).
 */
export function waveScale(distancePx: number): number {
  const d = Math.abs(distancePx);
  if (d >= WAVE_RADIUS_PX) return 1;
  const t = Math.cos(((d / WAVE_RADIUS_PX) * Math.PI) / 2);
  return 1 + (WAVE_MAX_SCALE - 1) * t * t;
}

/**
 * Caps applied by `commentPreview`. Outline labels truncate visually,
 * but agent comments can be tens of KB of
 * markdown — capping here keeps the flattened strings (and the aria-labels
 * derived from them) small instead of shipping the whole comment into the DOM.
 */
const PREVIEW_TITLE_MAX = 200;
const PREVIEW_BODY_MAX = 300;

/**
 * Flatten comment markdown into a plain-text preview: `title` is the first
 * non-empty line (bold in the card), `body` is the remaining lines joined
 * into one muted excerpt. Mirrors the chat list's `toPreview` flattening
 * (fences dropped, md tokens stripped) but keeps the first-line/body split
 * the minimap card renders.
 */
export function commentPreview(markdown: string): { title: string; body: string } {
  const lines = markdown
    .replace(/```[\s\S]*?```/g, " ")
    .split(/\r?\n/)
    .map((line) =>
      line
        .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
        .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
        .replace(/^\s*(?:[-+*]|\d+[.)])\s+/, "")
        .replace(/[#*`>~]/g, "")
        .replace(/\s+/g, " ")
        .trim(),
    )
    .filter(Boolean);
  return {
    title: (lines[0] ?? "").slice(0, PREVIEW_TITLE_MAX),
    body: lines.slice(1).join(" ").slice(0, PREVIEW_BODY_MAX),
  };
}

export interface ThreadMinimapThread {
  /** Root comment id — also the `comment-${id}` DOM anchor of the rendered row. */
  id: string;
  /** The thread's root comment entry (preview text + author fallback). */
  entry: TimelineEntry;
  /**
   * Whether the thread carries a resolution — derived by the caller with
   * `deriveThreadResolution`, so it covers both "Resolve thread" (root) and
   * "Resolve thread with comment" (reply), and stays true while the user has
   * a folded resolved thread expanded.
   */
  resolved: boolean;
  /** Unique authors across the root and every nested reply, in first-seen order. */
  participants: TimelineEntry[];
  /**
   * Live (non-deleted) replies in timeline order. Each gets a short tick and an
   * outline row; its `comment-${id}` anchor exists only once the thread is
   * expanded, which the caller's `onJump` takes care of.
   */
  replies: TimelineEntry[];
  /** The reply that resolved the thread ("Resolve thread with comment"), if any. */
  resolutionReplyId: string | null;
}

interface ThreadMinimapProps {
  threads: ThreadMinimapThread[];
  /** The issue detail scroll container; null until its callback ref populates. */
  scrollContainerEl: HTMLElement | null;
  /** Called with a thread root id or a reply id. */
  onJump: (commentId: string) => void;
  /** Positioning within the page (e.g. `absolute right-3 top-12 bottom-0`) — owned by the caller, like FindBar. */
  className?: string;
}

/** One rail tick / outline row: a thread root or one of its replies. */
interface MinimapItem {
  kind: "thread" | "reply";
  id: string;
  entry: TimelineEntry;
  /** The thread itself for a root, the enclosing thread for a reply. */
  thread: ThreadMinimapThread;
  /** Index of the enclosing thread's own item. */
  threadIndex: number;
}

function flattenThreads(threads: readonly ThreadMinimapThread[]): MinimapItem[] {
  const items: MinimapItem[] = [];
  for (const thread of threads) {
    const threadIndex = items.length;
    items.push({ kind: "thread", id: thread.id, entry: thread.entry, thread, threadIndex });
    for (const reply of thread.replies) {
      items.push({ kind: "reply", id: reply.id, entry: reply, thread, threadIndex });
    }
  }
  return items;
}

// ---------------------------------------------------------------------------
// useVisibleCommentIds — "which rail comments are on screen right now"
// ---------------------------------------------------------------------------
//
// Which ticked comments intersect the scroll viewport, so the rail can darken
// their ticks. A thread root's anchor wraps its whole card, so a thread tick
// stays dark while any of the thread is on screen; a reply's anchor is its own
// row. Deliberately the rail's alone: "on screen" is a set, not a point, and
// only a column of ticks can show a span without suggesting multiple selection.
//
// Computed from DOM rects on scroll/resize instead of an IntersectionObserver
// because Virtuoso mounts/unmounts rows while scrolling — an observer would
// lose its targets. Unmounted rows are by definition outside the (overscanned)
// viewport, and replies of a folded thread have no row at all, so "no element"
// correctly counts as not visible.

function sameIdSet(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}

function useVisibleCommentIds(
  commentIds: readonly string[],
  scrollContainerEl: HTMLElement | null,
): Set<string> {
  const [visibleIds, setVisibleIds] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    const container = scrollContainerEl;
    if (!container) return;

    let raf = 0;
    const compute = () => {
      raf = 0;
      const rect = container.getBoundingClientRect();
      const next = new Set<string>();
      for (const id of commentIds) {
        const el = document.getElementById(`comment-${id}`);
        if (!el) continue;
        const r = el.getBoundingClientRect();
        if (r.bottom > rect.top && r.top < rect.bottom) next.add(id);
      }
      setVisibleIds((prev) => (sameIdSet(prev, next) ? prev : next));
    };
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(compute);
    };

    compute();
    container.addEventListener("scroll", schedule, { passive: true });
    // Content height changes without scroll events: Virtuoso mounting rows
    // after first paint, streamed agent replies growing, window resizes.
    const ro = new ResizeObserver(schedule);
    ro.observe(container);
    if (container.firstElementChild) ro.observe(container.firstElementChild);
    return () => {
      container.removeEventListener("scroll", schedule);
      ro.disconnect();
      if (raf) cancelAnimationFrame(raf);
    };
  }, [commentIds, scrollContainerEl]);

  return visibleIds;
}

/** The item currently highlighted in the outline and rail. */
interface PreviewAnchor {
  /** Index into the flattened items (threads and replies). */
  index: number;
}

function MinimapTick({
  kind,
  index,
  label,
  inViewport,
  isHighlighted,
  onClick,
}: {
  kind: MinimapItem["kind"];
  /** Item index — how pointer and focus handlers map a tick back to its row. */
  index: number;
  label: string;
  inViewport: boolean;
  /** The corresponding outline row is active. */
  isHighlighted: boolean;
  onClick: React.MouseEventHandler<HTMLButtonElement>;
}) {
  const isReply = kind === "reply";
  return (
    <button
      type="button"
      aria-label={label}
      data-index={index}
      // Reply ticks stay out of the tab order: dozens of stops on a 6px rail
      // would bury the thread ticks. Keyboard users reach every reply through
      // the outline, which opens as soon as a thread tick takes focus.
      tabIndex={isReply ? -1 : undefined}
      onClick={onClick}
      // 20px wide, tick flushed to the right end: with the rail inset 12px
      // (see the caller's className) the strip spans 12–32px from the panel
      // edge, which clears a classic scrollbar's ~11px gutter on one side and
      // stops exactly at the content column's 32px padding on the other — so
      // it never sits on the scrollbar nor on body text, in either scrollbar
      // mode. Reply ticks take half the pitch, so a thread's replies pack
      // tightly under it and each thread reads as one group.
      className={cn(
        "group/tick flex w-5 cursor-pointer items-center justify-end focus-visible:outline-none",
        isReply ? "min-h-1 flex-[0_1_0.4375rem]" : "min-h-[5px] flex-[0_1_0.875rem]",
      )}
    >
      <span
        className={cn(
          // Enlargement is a right-anchored `scale` (compositor-friendly, and
          // what the JS wave writes inline), so ticks grow inward, away from
          // the scrollbar. The 100ms ease-out doubles as smoothing between
          // pointer samples and as the settle on leave. Reply ticks are half
          // the length of thread ticks and share their colors.
          "h-0.5 origin-right rounded-full transition-[scale,background-color] duration-100 ease-out",
          isReply ? "w-1.5" : "w-3",
          inViewport ? "bg-foreground/70" : "bg-muted-foreground/30",
          !isHighlighted && "group-hover/tick:bg-foreground",
          // CSS floor states for when no inline wave value is present:
          // the open card's tick stays grown while the pointer rests on the
          // card, keyboard focus grows without a pointer, and reduced-motion
          // swaps the wave for a plain hover grow.
          isHighlighted && "scale-x-[1.7] bg-brand",
          "group-focus-visible/tick:scale-x-[1.7]",
          !isHighlighted && "group-focus-visible/tick:bg-foreground",
          "motion-reduce:group-hover/tick:scale-x-[1.7]",
        )}
      />
    </button>
  );
}

export function ThreadMinimap({
  threads,
  scrollContainerEl,
  onJump,
  className,
}: ThreadMinimapProps) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
  const items = useMemo(() => flattenThreads(threads), [threads]);
  // Item indexes that get a rail tick: every comment while they fit, one per
  // thread beyond MAX_RAIL_TICKS.
  const railIndexes = useMemo(
    () =>
      items.length <= MAX_RAIL_TICKS
        ? items.map((_, i) => i)
        : items.flatMap((item, i) => (item.kind === "thread" ? [i] : [])),
    [items],
  );
  const railIds = useMemo(() => railIndexes.map((i) => items[i]!.id), [items, railIndexes]);
  const visibleIds = useVisibleCommentIds(railIds, scrollContainerEl);

  // Flattened previews, cached per comment by content so an unrelated timeline
  // update (reaction, new reply elsewhere) doesn't re-flatten every comment.
  const prevPreviewsRef = useRef<Map<string, { content: string | undefined; preview: { title: string; body: string } }>>(new Map());
  const previews = useMemo(() => {
    const next = new Map<string, { content: string | undefined; preview: { title: string; body: string } }>();
    const arr = items.map((item) => {
      const cached = prevPreviewsRef.current.get(item.id);
      const preview =
        cached && cached.content === item.entry.content
          ? cached.preview
          : commentPreview(item.entry.content ?? "");
      next.set(item.id, { content: item.entry.content, preview });
      return preview;
    });
    prevPreviewsRef.current = next;
    return arr;
  }, [items]);

  const shimRef = useRef<HTMLDivElement | null>(null);
  const navRef = useRef<HTMLElement | null>(null);
  const cardRef = useRef<HTMLDivElement | null>(null);

  // Hover wave + preview targeting. Pointer position lives in refs and ticks
  // are scaled with direct style writes so pointermove never re-renders the
  // component; the rAF guard coalesces bursts to one batched read-then-write
  // per frame. The same rect pass selects the corresponding outline row.
  const waveRafRef = useRef(0);
  const pointerYRef = useRef<number | null>(null);
  const reducedMotionRef = useRef(false);

  const [preview, setPreview] = useState<PreviewAnchor | null>(null);
  const previewRef = useRef<PreviewAnchor | null>(null);
  const pendingAnchorRef = useRef<PreviewAnchor | null>(null);
  const openTimerRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const showPreview = useCallback((anchor: PreviewAnchor | null) => {
    previewRef.current = anchor;
    setPreview((prev) =>
      prev?.index === anchor?.index ? prev : anchor,
    );
  }, []);

  useEffect(() => {
    reducedMotionRef.current = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    return () => {
      if (waveRafRef.current) cancelAnimationFrame(waveRafRef.current);
      if (openTimerRef.current !== null) window.clearTimeout(openTimerRef.current);
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    };
  }, []);

  const cancelClose = useCallback(() => {
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  }, []);
  const scheduleClose = useCallback(() => {
    cancelClose();
    if (openTimerRef.current !== null) {
      window.clearTimeout(openTimerRef.current);
      openTimerRef.current = null;
    }
    closeTimerRef.current = window.setTimeout(() => {
      closeTimerRef.current = null;
      if (!shimRef.current?.contains(document.activeElement)) showPreview(null);
    }, PREVIEW_CLOSE_DELAY_MS);
  }, [cancelClose, showPreview]);

  const handleJump = useCallback((commentId: string, event: React.MouseEvent<HTMLButtonElement>) => {
    // Mouse clicks must not pin a hover outline through leftover button focus.
    // Keyboard activation keeps focus so the reader can continue navigating.
    if (event.detail > 0) {
      event.currentTarget.blur();
      // Blur schedules a close; keep the card until the pointer actually leaves.
      cancelClose();
    }
    onJump(commentId);
  }, [cancelClose, onJump]);

  const runWave = useCallback(() => {
    waveRafRef.current = 0;
    const nav = navRef.current;
    const shim = shimRef.current;
    if (!nav || !shim) return;
    const y = pointerYRef.current;
    const buttons = nav.querySelectorAll<HTMLButtonElement>("button");
    // Read pass, then write pass — never interleaved, one reflow at most.
    const scales: string[] = [];
    let nearest: { index: number; dist: number } | null = null;
    buttons.forEach((b) => {
      if (y === null) {
        scales.push("");
        return;
      }
      const r = b.getBoundingClientRect();
      const centerY = r.top + r.height / 2;
      const dist = Math.abs(y - centerY);
      const s = reducedMotionRef.current ? 1 : waveScale(y - centerY);
      scales.push(s > 1.001 ? `${s.toFixed(3)} 1` : "");
      if (!nearest || dist < nearest.dist) nearest = { index: Number(b.dataset.index), dist };
    });
    buttons.forEach((b, i) => {
      const tick = b.firstElementChild as HTMLElement | null;
      if (!tick) return;
      const s = scales[i]!;
      // Clearing the inline value hands control back to the CSS floor states
      // (open-card tick / focus-visible / reduced-motion hover).
      if (s) tick.style.setProperty("scale", s);
      else tick.style.removeProperty("scale");
    });

    if (y === null || !nearest) return;
    const { index } = nearest as { index: number };
    const anchor: PreviewAnchor = { index };
    pendingAnchorRef.current = anchor;
    if (previewRef.current) {
      // Already open: gliding highlights the matching row without moving the card.
      showPreview(anchor);
    } else if (openTimerRef.current === null) {
      openTimerRef.current = window.setTimeout(() => {
        openTimerRef.current = null;
        if (pointerYRef.current !== null) showPreview(pendingAnchorRef.current);
      }, PREVIEW_OPEN_DELAY_MS);
    }
  }, [showPreview]);
  const scheduleWave = useCallback(() => {
    if (!waveRafRef.current) waveRafRef.current = requestAnimationFrame(runWave);
  }, [runWave]);
  const handleWaveMove = useCallback(
    (e: React.PointerEvent) => {
      cancelClose();
      pointerYRef.current = e.clientY;
      scheduleWave();
    },
    [cancelClose, scheduleWave],
  );
  const handleWaveLeave = useCallback(() => {
    pointerYRef.current = null;
    scheduleWave();
    scheduleClose();
  }, [scheduleWave, scheduleClose]);

  // Keyboard parity: focusing a tick opens its outline row immediately —
  // there is no pointer, so there is no accidental-hover to debounce.
  const handleFocus = useCallback(
    (e: React.FocusEvent) => {
      const btn = (e.target as HTMLElement).closest("button");
      if (!btn) return;
      cancelClose();
      const index = Number(btn.dataset.index);
      if (Number.isNaN(index)) return;
      showPreview({ index });
    },
    [cancelClose, showPreview],
  );

  useEffect(() => {
    const card = cardRef.current;
    if (!card || !preview) return;
    // Rail navigation should reveal its row in a long outline. Moving within
    // the list itself must leave its scroll position under the reader's control.
    if (pointerYRef.current === null && !navRef.current?.contains(document.activeElement)) return;
    const row = card.querySelectorAll("li")[preview.index];
    if (!row) return;
    if (row.offsetTop < card.scrollTop) card.scrollTop = row.offsetTop;
    else if (row.offsetTop + row.offsetHeight > card.scrollTop + card.clientHeight) {
      card.scrollTop = row.offsetTop + row.offsetHeight - card.clientHeight;
    }
  }, [preview]);

  if (threads.length < MIN_THREADS) return null;

  const actorName = (entry: TimelineEntry) =>
    entry.actor_name || getActorName(entry.actor_type, entry.actor_id);
  const itemTitle = (item: MinimapItem, index: number) =>
    isDeletedComment(item.entry)
      ? t(($) => $.comment.deleted_placeholder)
      : previews[index]!.title || actorName(item.entry);
  // Accessible name shared by a tick and its outline row: resolution is
  // announced on thread ticks, a reply names its author up front.
  const itemLabel = (item: MinimapItem, index: number) => {
    const title = itemTitle(item, index);
    if (item.kind === "reply") {
      return t(($) => $.detail.thread_nav_reply_label, { author: actorName(item.entry), title });
    }
    return item.thread.resolved ? t(($) => $.detail.thread_nav_resolved_label, { title }) : title;
  };
  const avatarFor = (entry: TimelineEntry, size: "xs" | "sm") => {
    const name = actorName(entry);
    const avatarUrl = entry.actor_avatar_url?.startsWith("/")
      ? resolvePublicFileUrl(entry.actor_avatar_url)
      : entry.actor_avatar_url ?? getActorAvatarUrl(entry.actor_type, entry.actor_id);
    return (
      <ActorAvatar
        name={name}
        initials={getActorInitials(entry.actor_type, entry.actor_id, name)}
        avatarUrl={avatarUrl}
        isAgent={entry.actor_type === "agent"}
        size={size}
      />
    );
  };
  // With reply ticks dropped, a highlighted reply row lights its thread's tick.
  const highlightedTick = preview
    ? items.length <= MAX_RAIL_TICKS
      ? preview.index
      : items[preview.index]?.threadIndex
    : undefined;

  return (
    // Positioning shim; only the nav and the card take pointer events so the
    // strip never blocks content clicks.
    <div
      ref={shimRef}
      onKeyDown={(event) => {
        if (event.key !== "Escape") return;
        event.preventDefault();
        event.stopPropagation();
        const activeIndex = previewRef.current?.index;
        if (cardRef.current?.contains(document.activeElement) && activeIndex !== undefined) {
          const nav = navRef.current;
          const tick =
            nav?.querySelector<HTMLButtonElement>(`button[data-index="${activeIndex}"]`) ??
            nav?.querySelector<HTMLButtonElement>(`button[data-index="${items[activeIndex]?.threadIndex}"]`);
          tick?.focus();
        }
        cancelClose();
        if (openTimerRef.current !== null) {
          window.clearTimeout(openTimerRef.current);
          openTimerRef.current = null;
        }
        showPreview(null);
      }}
      // z-30, like the find bar: the outline must paint over every sticky
      // affordance pinned in the timeline (comment headers z-10, resolve
      // collapse bars z-20), which jumping into a resolved thread now pins.
      className={cn("pointer-events-none z-30 flex flex-col justify-center py-6", className)}
    >
      <nav
        ref={navRef}
        aria-label={t(($) => $.detail.thread_nav_label)}
        onPointerMove={handleWaveMove}
        onPointerLeave={handleWaveLeave}
        onFocusCapture={handleFocus}
        onBlurCapture={scheduleClose}
        // Bounded height + shrinkable ticks: when comments outgrow the rail,
        // flex compresses the spacing (down to min-h) instead of overflowing.
        className="pointer-events-auto flex max-h-full flex-col overflow-hidden"
      >
        {railIndexes.map((i) => {
          const item = items[i]!;
          return (
            <MinimapTick
              key={item.id}
              kind={item.kind}
              index={i}
              label={itemLabel(item, i)}
              inViewport={visibleIds.has(item.id)}
              isHighlighted={highlightedTick === i}
              onClick={(event) => handleJump(item.id, event)}
            />
          );
        })}
      </nav>

      {preview && (
        <div
          ref={cardRef}
          onPointerEnter={cancelClose}
          onPointerLeave={scheduleClose}
          onFocusCapture={cancelClose}
          onBlurCapture={scheduleClose}
          className="pointer-events-auto absolute right-8 top-1/2 max-h-[calc(100%-3rem)] w-80 max-w-[calc(100vw-4rem)] -translate-y-1/2 overflow-y-auto overscroll-contain rounded-xl bg-popover p-2 text-body text-popover-foreground shadow-lg ring-1 ring-foreground/10"
        >
          {/* One <li> per item, replies included, so row index === item index. */}
          <ul>
            {items.map((item, index) => {
              const title = itemTitle(item, index);
              const rowProps = {
                type: "button" as const,
                onPointerEnter: () => showPreview({ index }),
                onFocus: () => showPreview({ index }),
                onClick: (event: React.MouseEvent<HTMLButtonElement>) => handleJump(item.id, event),
                "data-active": preview.index === index || undefined,
                "aria-label": itemLabel(item, index),
              };

              if (item.kind === "reply") {
                return (
                  // The guide line runs down the left of consecutive reply rows,
                  // tying them to the thread row above.
                  <li
                    key={item.id}
                    className="relative ml-3 before:absolute before:inset-y-0 before:left-0 before:w-px before:bg-surface-border"
                  >
                    <button
                      {...rowProps}
                      className="flex w-full items-center gap-2 rounded-md py-1 pl-3 pr-3 text-left text-label text-muted-foreground transition-colors hover:bg-surface-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring data-active:font-medium data-active:text-brand"
                    >
                      <span className="inline-flex shrink-0" aria-hidden="true">
                        {avatarFor(item.entry, "xs")}
                      </span>
                      <span className="min-w-0 flex-1 truncate">{title}</span>
                      {item.thread.resolutionReplyId === item.id && (
                        <CheckCircle2
                          className="size-3 shrink-0 text-success"
                          aria-label={t(($) => $.comment.resolve.resolution_badge)}
                        />
                      )}
                      <span className="shrink-0 text-micro font-normal tabular-nums text-muted-foreground">
                        {timeAgo(item.entry.created_at)}
                      </span>
                    </button>
                  </li>
                );
              }

              const thread = item.thread;
              const participantNames = thread.participants.map(actorName);
              return (
                <li key={item.id}>
                  <button
                    {...rowProps}
                    aria-description={participantNames.join(", ") || undefined}
                    className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-body text-muted-foreground transition-colors hover:bg-surface-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring data-active:font-medium data-active:text-brand"
                  >
                    <span className="flex min-w-0 flex-1 items-center gap-1.5">
                      <span className="truncate">{title}</span>
                      {thread.resolved && (
                        <CheckCircle2
                          className="size-3.5 shrink-0 text-success"
                          aria-label={t(($) => $.comment.resolve.thread_resolved_badge)}
                        />
                      )}
                    </span>
                    <span className="inline-flex shrink-0 items-center -space-x-1.5" aria-hidden="true">
                      {thread.participants.slice(0, 3).map((participant, participantIndex) => (
                        <span
                          key={`${participant.actor_type}:${participant.actor_id}`}
                          title={participantNames[participantIndex]!}
                          className="inline-flex rounded-full ring-2 ring-popover"
                        >
                          {avatarFor(participant, "sm")}
                        </span>
                      ))}
                      {thread.participants.length > 3 && (
                        <span
                          title={participantNames.slice(3).join(", ")}
                          className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-muted px-0.5 text-micro font-medium tabular-nums text-muted-foreground ring-2 ring-popover"
                        >
                          +{thread.participants.length - 3}
                        </span>
                      )}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
}
