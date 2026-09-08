import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CheckCircle2 } from "lucide-react";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// ThreadMinimap — quick-jump rail with a complete thread outline.
// The rail shows viewport position; hovering or focusing any tick opens one
// stationary, scrollable list of every thread title. Rows jump to the same
// timeline anchors as the ticks, including folded resolved threads.

/** Minimum number of threads before the rail is worth its pixels. */
const MIN_THREADS = 2;

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
}

interface ThreadMinimapProps {
  threads: ThreadMinimapThread[];
  /** The issue detail scroll container; null until its callback ref populates. */
  scrollContainerEl: HTMLElement | null;
  onJump: (threadId: string) => void;
  /** Positioning within the page (e.g. `absolute right-3 top-12 bottom-0`) — owned by the caller, like FindBar. */
  className?: string;
}

// ---------------------------------------------------------------------------
// useVisibleThreadIds — "which comment threads are on screen right now"
// ---------------------------------------------------------------------------
//
// Which threads intersect the scroll viewport, so the rail can darken their
// ticks. Deliberately the rail's alone: "on screen" is a set, not a point, and
// only a column of ticks can show a span without suggesting multiple selection.
//
// Computed from DOM rects on scroll/resize instead of an IntersectionObserver
// because Virtuoso mounts/unmounts rows while scrolling — an observer would
// lose its targets. Unmounted rows are by definition outside the (overscanned)
// viewport, so "no element" correctly counts as not visible.

function sameIdSet(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}

function useVisibleThreadIds(
  threadIds: readonly string[],
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
      for (const id of threadIds) {
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
  }, [threadIds, scrollContainerEl]);

  return visibleIds;
}

/** The thread currently highlighted in the outline and rail. */
interface PreviewAnchor {
  index: number;
}

function MinimapTick({
  label,
  inViewport,
  isHighlighted,
  onClick,
}: {
  label: string;
  inViewport: boolean;
  /** The corresponding outline row is active. */
  isHighlighted: boolean;
  onClick: React.MouseEventHandler<HTMLButtonElement>;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      // 20px wide, tick flushed to the right end: with the rail inset 12px
      // (see the caller's className) the strip spans 12–32px from the panel
      // edge, which clears a classic scrollbar's ~11px gutter on one side and
      // stops exactly at the content column's 32px padding on the other — so
      // it never sits on the scrollbar nor on body text, in either scrollbar
      // mode.
      className="group/tick flex min-h-[5px] w-5 flex-[0_1_0.875rem] cursor-pointer items-center justify-end focus-visible:outline-none"
    >
      <span
        className={cn(
          // Enlargement is a right-anchored `scale` (compositor-friendly, and
          // what the JS wave writes inline), so ticks grow inward, away from
          // the scrollbar. The 100ms ease-out doubles as smoothing between
          // pointer samples and as the settle on leave.
          "h-0.5 w-3 origin-right rounded-full transition-[scale,background-color] duration-100 ease-out",
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
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
  const threadIds = useMemo(() => threads.map((th) => th.id), [threads]);
  const visibleIds = useVisibleThreadIds(threadIds, scrollContainerEl);

  // Flattened previews, cached per thread by content so an unrelated timeline
  // update (reaction, new reply elsewhere) doesn't re-flatten every comment.
  const prevPreviewsRef = useRef<Map<string, { content: string | undefined; preview: { title: string; body: string } }>>(new Map());
  const previews = useMemo(() => {
    const next = new Map<string, { content: string | undefined; preview: { title: string; body: string } }>();
    const arr = threads.map((th) => {
      const cached = prevPreviewsRef.current.get(th.id);
      const preview =
        cached && cached.content === th.entry.content
          ? cached.preview
          : commentPreview(th.entry.content ?? "");
      next.set(th.id, { content: th.entry.content, preview });
      return preview;
    });
    prevPreviewsRef.current = next;
    return arr;
  }, [threads]);

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

  const handleJump = useCallback((threadId: string, event: React.MouseEvent<HTMLButtonElement>) => {
    // Mouse clicks must not pin a hover outline through leftover button focus.
    // Keyboard activation keeps focus so the reader can continue navigating.
    if (event.detail > 0) {
      event.currentTarget.blur();
      // Blur schedules a close; keep the card until the pointer actually leaves.
      cancelClose();
    }
    onJump(threadId);
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
    buttons.forEach((b, i) => {
      if (y === null) {
        scales.push("");
        return;
      }
      const r = b.getBoundingClientRect();
      const centerY = r.top + r.height / 2;
      const dist = Math.abs(y - centerY);
      const s = reducedMotionRef.current ? 1 : waveScale(y - centerY);
      scales.push(s > 1.001 ? `${s.toFixed(3)} 1` : "");
      if (!nearest || dist < nearest.dist) nearest = { index: i, dist };
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
      const nav = navRef.current;
      const shim = shimRef.current;
      const btn = (e.target as HTMLElement).closest("button");
      if (!nav || !shim || !btn) return;
      cancelClose();
      const buttons = [...nav.querySelectorAll<HTMLButtonElement>("button")];
      const index = buttons.indexOf(btn as HTMLButtonElement);
      if (index < 0) return;
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
          navRef.current?.querySelectorAll("button")[activeIndex]?.focus();
        }
        cancelClose();
        if (openTimerRef.current !== null) {
          window.clearTimeout(openTimerRef.current);
          openTimerRef.current = null;
        }
        showPreview(null);
      }}
      className={cn("pointer-events-none z-10 flex flex-col justify-center py-6", className)}
    >
      <nav
        ref={navRef}
        aria-label={t(($) => $.detail.thread_nav_label)}
        onPointerMove={handleWaveMove}
        onPointerLeave={handleWaveLeave}
        onFocusCapture={handleFocus}
        onBlurCapture={scheduleClose}
        // Bounded height + shrinkable ticks: when threads outgrow the rail,
        // flex compresses the spacing (down to min-h) instead of overflowing.
        className="pointer-events-auto flex max-h-full flex-col overflow-hidden"
      >
        {threads.map((thread, i) => {
          const title =
            previews[i]!.title ||
            thread.entry.actor_name ||
            getActorName(thread.entry.actor_type, thread.entry.actor_id);
          return (
            <MinimapTick
              key={thread.id}
              // Announce resolution on the tick as well as in the outline.
              label={
                thread.resolved
                  ? t(($) => $.detail.thread_nav_resolved_label, { title })
                  : title
              }
              inViewport={visibleIds.has(thread.id)}
              isHighlighted={preview?.index === i}
              onClick={(event) => handleJump(thread.id, event)}
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
          <ul>
            {threads.map((thread, index) => {
              const title = previews[index]!.title || thread.entry.actor_name ||
                getActorName(thread.entry.actor_type, thread.entry.actor_id);
              const participantNames = thread.participants.map((participant) =>
                participant.actor_name || getActorName(participant.actor_type, participant.actor_id),
              );
              return (
                <li key={thread.id}>
                  <button
                    type="button"
                    onPointerEnter={() => showPreview({ index })}
                    onFocus={() => showPreview({ index })}
                    onClick={(event) => handleJump(thread.id, event)}
                    data-active={preview.index === index || undefined}
                    aria-label={thread.resolved
                      ? t(($) => $.detail.thread_nav_resolved_label, { title })
                      : title}
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
                      {thread.participants.slice(0, 3).map((participant, participantIndex) => {
                        const name = participantNames[participantIndex]!;
                        const avatarUrl = participant.actor_avatar_url?.startsWith("/")
                          ? resolvePublicFileUrl(participant.actor_avatar_url)
                          : participant.actor_avatar_url ?? getActorAvatarUrl(participant.actor_type, participant.actor_id);
                        return (
                          <span
                            key={`${participant.actor_type}:${participant.actor_id}`}
                            title={name}
                            className="inline-flex rounded-full ring-2 ring-popover"
                          >
                            <ActorAvatar
                              name={name}
                              initials={getActorInitials(participant.actor_type, participant.actor_id, name)}
                              avatarUrl={avatarUrl}
                              isAgent={participant.actor_type === "agent"}
                              size="sm"
                            />
                          </span>
                        );
                      })}
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
