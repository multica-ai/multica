import { useEffect, useMemo, useState } from "react";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
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
// here" minimap. Hovering a tick grows it and opens a preview card (bold
// first line + muted body excerpt); clicking jumps the timeline to that
// thread.
//
// The rail deliberately skips activity groups: they are timeline noise, not
// navigation destinations.

/** Minimum number of threads before the rail is worth its pixels. */
const MIN_THREADS = 2;

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

function MinimapTick({
  thread,
  inViewport,
  onJump,
}: {
  thread: ThreadMinimapThread;
  inViewport: boolean;
  onJump: (threadId: string) => void;
}) {
  const { getActorName } = useActorName();
  const { title, body } = useMemo(
    () => commentPreview(thread.entry.content ?? ""),
    [thread.entry.content],
  );
  // Attachment-only comments flatten to nothing — fall back to the author
  // name so the tick still has an accessible name and the card isn't blank.
  const label = title || getActorName(thread.entry.actor_type, thread.entry.actor_id);

  return (
    <HoverCard>
      <HoverCardTrigger
        render={
          <button
            type="button"
            aria-label={label}
            onClick={() => onJump(thread.id)}
          />
        }
        // Snappier than the 600ms default — the rail is a scanning surface;
        // short closeDelay keeps neighbouring cards from overlapping.
        delay={150}
        closeDelay={100}
        className="group/tick flex min-h-[5px] w-6 flex-[0_1_0.875rem] cursor-pointer items-center focus-visible:outline-none"
      >
        <span
          className={cn(
            "h-0.5 w-3 rounded-full transition-all duration-150",
            inViewport ? "bg-foreground/70" : "bg-muted-foreground/30",
            "group-hover/tick:w-[18px] group-hover/tick:bg-foreground",
            "group-data-[popup-open]/tick:w-[18px] group-data-[popup-open]/tick:bg-foreground",
            "group-focus-visible/tick:w-[18px] group-focus-visible/tick:bg-foreground",
          )}
        />
      </HoverCardTrigger>
      <HoverCardContent side="right" align="center" sideOffset={10} className="w-72">
        <p className="truncate text-sm font-semibold text-foreground">{label}</p>
        {body && (
          <p className="mt-1 line-clamp-3 text-sm text-muted-foreground">{body}</p>
        )}
      </HoverCardContent>
    </HoverCard>
  );
}

export function ThreadMinimap({ threads, scrollContainerEl, onJump, className }: ThreadMinimapProps) {
  const { t } = useT("issues");
  const visibleIds = useVisibleThreadIds(threads, scrollContainerEl);

  if (threads.length < MIN_THREADS) return null;

  return (
    // Positioning shim; only the nav itself takes pointer events so the
    // strip never blocks content clicks.
    <div className={cn("pointer-events-none z-10 flex flex-col justify-center py-6", className)}>
      <nav
        aria-label={t(($) => $.detail.thread_nav_label)}
        // Bounded height + shrinkable ticks: when threads outgrow the rail,
        // flex compresses the spacing (down to min-h) instead of overflowing.
        className="pointer-events-auto flex max-h-full flex-col overflow-hidden"
      >
        {threads.map((thread) => (
          <MinimapTick
            key={thread.id}
            thread={thread}
            inViewport={visibleIds.has(thread.id)}
            onJump={onJump}
          />
        ))}
      </nav>
    </div>
  );
}
