import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// ThreadMinimap — Linear-style quick-jump rail for comment threads
// ---------------------------------------------------------------------------
//
// A vertical column of tick marks overlaid on the left edge of the issue
// detail scroll area, one tick per top-level comment thread (folded resolved
// bars included — they are jump targets too). Ticks whose thread is currently
// inside the scroll viewport render darker, so the rail doubles as a "you are
// here" minimap. Hovering magnifies ticks in a Dock-style wave around the
// cursor (the hovered tick peaks, neighbours taper off) and shows a preview
// card (bold first line + muted body excerpt); clicking jumps the timeline
// to that thread.
//
// The preview is ONE card owned by the rail, not a popover per tick: the
// open-intent delay is paid once when the pointer enters the rail, and while
// the pointer glides across ticks the card swaps content instantly and
// slides to the hovered tick. Per-tick popovers re-paid the open delay and
// exit/enter animations on every tick crossed, which read as lag when
// scanning the rail continuously.
//
// The rail deliberately skips activity groups: they are timeline noise, not
// navigation destinations.

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
 * Caps applied by `commentPreview`. The preview card clamps visually
 * (`truncate` / `line-clamp-3`), but agent comments can be tens of KB of
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
}

interface ThreadMinimapProps {
  threads: ThreadMinimapThread[];
  /** The issue detail scroll container; null until its callback ref populates. */
  scrollContainerEl: HTMLElement | null;
  onJump: (threadId: string) => void;
  /** Positioning within the page (e.g. `absolute left-2 top-12 bottom-0`) — owned by the caller, like FindBar. */
  className?: string;
}

function sameIdSet(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}

/**
 * Which threads currently intersect the scroll viewport. Computed from DOM
 * rects on scroll/resize instead of an IntersectionObserver because Virtuoso
 * mounts/unmounts rows while scrolling — an observer would lose its targets.
 * Unmounted rows are by definition outside the (overscanned) viewport, so
 * "no element" correctly counts as not visible.
 */
function useVisibleThreadIds(
  threads: ThreadMinimapThread[],
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
      for (const t of threads) {
        const el = document.getElementById(`comment-${t.id}`);
        if (!el) continue;
        const r = el.getBoundingClientRect();
        if (r.bottom > rect.top && r.top < rect.bottom) next.add(t.id);
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
  }, [threads, scrollContainerEl]);

  return visibleIds;
}

/** The rail-owned preview card's target: which tick, anchored at which shim-relative Y. */
interface PreviewAnchor {
  index: number;
  y: number;
}

function MinimapTick({
  label,
  inViewport,
  isPreviewOpen,
  onClick,
}: {
  label: string;
  inViewport: boolean;
  /** This tick's preview is the open card — hold the grown state even when the pointer is on the card. */
  isPreviewOpen: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      className="group/tick flex min-h-[5px] w-6 flex-[0_1_0.875rem] cursor-pointer items-center focus-visible:outline-none"
    >
      <span
        className={cn(
          // Enlargement is a left-anchored `scale` (compositor-friendly, and
          // what the JS wave writes inline). The 100ms ease-out doubles as
          // smoothing between pointer samples and as the settle on leave.
          "h-0.5 w-3 origin-left rounded-full transition-[scale,background-color] duration-100 ease-out",
          inViewport ? "bg-foreground/70" : "bg-muted-foreground/30",
          "group-hover/tick:bg-foreground",
          // CSS floor states for when no inline wave value is present:
          // the open card's tick stays grown while the pointer rests on the
          // card, keyboard focus grows without a pointer, and reduced-motion
          // swaps the wave for a plain hover grow.
          isPreviewOpen && "scale-x-[1.7] bg-foreground",
          "group-focus-visible/tick:scale-x-[1.7] group-focus-visible/tick:bg-foreground",
          "motion-reduce:group-hover/tick:scale-x-[1.7]",
        )}
      />
    </button>
  );
}

export function ThreadMinimap({ threads, scrollContainerEl, onJump, className }: ThreadMinimapProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const visibleIds = useVisibleThreadIds(threads, scrollContainerEl);

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
  // per frame. The same rect pass derives which tick the card should anchor
  // to, so the card and the wave can never disagree about the hovered tick.
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
      prev?.index === anchor?.index && prev?.y === anchor?.y ? prev : anchor,
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
      showPreview(null);
    }, PREVIEW_CLOSE_DELAY_MS);
  }, [cancelClose, showPreview]);

  const runWave = useCallback(() => {
    waveRafRef.current = 0;
    const nav = navRef.current;
    const shim = shimRef.current;
    if (!nav || !shim) return;
    const y = pointerYRef.current;
    const buttons = nav.querySelectorAll<HTMLButtonElement>("button");
    // Read pass, then write pass — never interleaved, one reflow at most.
    const shimTop = shim.getBoundingClientRect().top;
    const shimHeight = shim.clientHeight;
    const cardHalf = (cardRef.current?.offsetHeight ?? 96) / 2;
    const scales: string[] = [];
    let nearest: { index: number; centerY: number; dist: number } | null = null;
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
      if (!nearest || dist < nearest.dist) nearest = { index: i, centerY, dist };
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

    // Preview targeting from the same pass. Clamp the anchor so the card
    // never sticks out of the rail's column at the extremes.
    if (y === null || !nearest) return;
    const { index, centerY } = nearest as { index: number; centerY: number };
    const anchor: PreviewAnchor = {
      index,
      y: Math.min(Math.max(centerY - shimTop, cardHalf + 6), shimHeight - cardHalf - 6),
    };
    pendingAnchorRef.current = anchor;
    if (previewRef.current) {
      // Already open: gliding retargets the card instantly — no re-delay.
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

  // Keyboard parity: focusing a tick anchors the card to it immediately —
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
      const r = btn.getBoundingClientRect();
      const cardHalf = (cardRef.current?.offsetHeight ?? 96) / 2;
      const y = r.top + r.height / 2 - shim.getBoundingClientRect().top;
      showPreview({
        index,
        y: Math.min(Math.max(y, cardHalf + 6), shim.clientHeight - cardHalf - 6),
      });
    },
    [cancelClose, showPreview],
  );

  if (threads.length < MIN_THREADS) return null;

  const activeThread = preview ? threads[preview.index] : undefined;
  const activePreview = preview ? previews[preview.index] : undefined;
  const activeTitle = activeThread && activePreview
    ? activePreview.title ||
      getActorName(activeThread.entry.actor_type, activeThread.entry.actor_id)
    : undefined;

  return (
    // Positioning shim; only the nav and the card take pointer events so the
    // strip never blocks content clicks.
    <div ref={shimRef} className={cn("pointer-events-none z-10 flex flex-col justify-center py-6", className)}>
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
        {threads.map((thread, i) => (
          <MinimapTick
            key={thread.id}
            label={
              previews[i]!.title ||
              getActorName(thread.entry.actor_type, thread.entry.actor_id)
            }
            inViewport={visibleIds.has(thread.id)}
            isPreviewOpen={preview?.index === i}
            onClick={() => onJump(thread.id)}
          />
        ))}
      </nav>

      {/* The rail's single preview card. Mounted without an enter animation
          (the open-intent delay already gates accidental flashes; once the
          user waited, showing content instantly is the responsive choice)
          and slid between ticks with a short transform transition. Hovering
          the card keeps it open so its text stays selectable. */}
      {preview && activeThread && activePreview && (
        <div
          ref={cardRef}
          onPointerEnter={cancelClose}
          onPointerLeave={scheduleClose}
          className="pointer-events-auto absolute left-9 top-0 w-72 rounded-lg bg-popover p-2.5 text-sm text-popover-foreground shadow-md ring-1 ring-foreground/10 transition-transform duration-150 ease-out motion-reduce:transition-none"
          style={{ transform: `translateY(${preview.y}px) translateY(-50%)` }}
        >
          <p className="truncate text-sm font-semibold text-foreground">{activeTitle}</p>
          {activePreview.body && (
            <p className="mt-1 line-clamp-3 text-sm text-muted-foreground">{activePreview.body}</p>
          )}
        </div>
      )}
    </div>
  );
}
