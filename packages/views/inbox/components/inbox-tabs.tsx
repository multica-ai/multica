"use client";

import { useLayoutEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react";
import { animate, motion, useMotionValue, type AnimationPlaybackControls, type MotionValue } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { AlarmClock, Archive, Inbox } from "@multica/ui/icons";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { InboxTab } from "./inbox-view";

const TABS: readonly InboxTab[] = ["inbox", "snoozed", "archived"];

const TAB_ICONS: Record<InboxTab, typeof Inbox> = {
  inbox: Inbox,
  snoozed: AlarmClock,
  archived: Archive,
};

/**
 * The Inbox's three lists as tabs: the live Inbox with its unread count, the
 * issues snoozed out of it with theirs, and the archive. The Work panel's
 * Inbox section shows them in its header; a phone's Inbox page, in its own.
 *
 * In the section header only the open tab carries its name: the other two are
 * icons named by a tooltip, so at the panel's default width no label is cut
 * down to its first letter. The unread count stays on the Inbox
 * icon; the snoozed count moves into its tooltip. On a switch the highlight
 * slides to the new tab while its name unfolds and the old one folds back
 * into its icon, so the name visibly moves where it went.
 */
export function InboxTabs({
  tab,
  onSelect,
  onPrefetch,
  unread,
  snoozed,
  expanded,
  size = "section",
  className,
}: {
  tab: InboxTab;
  onSelect: (tab: InboxTab) => void;
  /** Called when a tab is pointed at or focused, so its list can load before the click. */
  onPrefetch?: (tab: InboxTab) => void;
  /** Left out (or 0) to show no count. */
  unread?: number;
  snoozed?: number;
  /** Set when selecting the open tab again collapses the list it heads. */
  expanded?: boolean;
  /** `section` for the Work panel's header, `page` for a page header. */
  size?: "section" | "page";
  className?: string;
}) {
  const { t } = useT("inbox");
  const reduceMotion = useReducedMotion();
  const listRef = useRef<HTMLDivElement>(null);
  const section = size === "section";
  // The tab folding back into its icon keeps shrinking with the others until
  // it has folded, rather than holding its place against the open one.
  const [folding, setFolding] = useState<InboxTab | null>(null);
  const highlight = useTabSwitchMotion(listRef, tab, section, reduceMotion ? 0 : UI_MOTION_DURATION.standard, setFolding);
  const labels: Record<InboxTab, string> = {
    inbox: t(($) => $.page.title),
    snoozed: t(($) => $.list.snoozed_title),
    archived: t(($) => $.list.archived_title),
  };
  const counts: Partial<Record<InboxTab, number>> = { inbox: unread, snoozed };
  // Arrow keys move between the tabs, as a tab list's do.
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const at = TABS.indexOf(tab) + (event.key === "ArrowRight" ? 1 : -1);
    const next = TABS[(at + TABS.length) % TABS.length]!;
    onSelect(next);
    event.currentTarget.querySelector<HTMLElement>(`[data-inbox-tab="${next}"]`)?.focus();
  };

  return (
    <div
      ref={listRef}
      role="tablist"
      aria-label={labels.inbox}
      onKeyDown={onKeyDown}
      className={cn("relative flex min-w-0 items-center gap-0.5", section ? "-ml-1.5" : "-ml-2", className)}
    >
      {section && (
        <motion.span
          aria-hidden
          className="absolute top-0 left-0 h-5 rounded-md bg-surface-selected"
          style={{ x: highlight.x, width: highlight.width }}
        />
      )}
      {TABS.map((id) => {
        const active = id === tab;
        const count = counts[id] ?? 0;
        const iconOnly = size === "section" && !active;
        const countClass = cn("shrink-0 tabular-nums", id === "inbox" ? "text-brand" : "text-faint-foreground");
        const Icon = TAB_ICONS[id];
        const button = (
          <button
            type="button"
            role="tab"
            data-inbox-tab={id}
            aria-selected={active}
            aria-expanded={active ? expanded : undefined}
            tabIndex={active ? 0 : -1}
            onClick={() => onSelect(id)}
            onPointerEnter={onPrefetch && !active ? () => onPrefetch(id) : undefined}
            onFocus={onPrefetch && !active ? () => onPrefetch(id) : undefined}
            className={cn(
              "flex items-center rounded-md outline-none transition-colors focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-ring",
              section ? "relative h-5 px-1" : "h-7 gap-1 px-2",
              // The open tab gives up width before the icons do; a label
              // still too long for the panel (Spanish, French) truncates.
              active || id === folding ? "min-w-0" : "shrink-0",
              active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
              active && !section && "bg-surface-selected",
            )}
          >
            {section ? (
              // A closed tab's name is folded to nothing rather than hidden,
              // so it still names the tab; a switch animates both parts.
              <>
                <span data-tab-icon aria-hidden className={cn("flex shrink-0 items-center overflow-hidden", active && "w-0 opacity-0")}>
                  <Icon className="size-3.5 shrink-0" />
                </span>
                <span data-tab-label className={cn("flex min-w-0 items-center overflow-hidden", !active && "w-0 opacity-0")}>
                  <span className="truncate px-0.5">{labels[id]}</span>
                  {id !== "inbox" && count > 0 && <span className={cn(countClass, "ml-0.5 pr-0.5")}>{count}</span>}
                </span>
                {id === "inbox" && count > 0 && <span className={cn(countClass, active ? "px-0.5" : "ml-1")}>{count}</span>}
              </>
            ) : (
              <>
                <span className="truncate">{labels[id]}</span>
                {count > 0 && <span className={countClass}>{count}</span>}
              </>
            )}
          </button>
        );
        // Every tab keeps the same element as it opens and closes, so arrow
        // keys keep focus on it; only the icons' tooltips are enabled.
        return (
          <Tooltip key={id} disabled={!iconOnly}>
            <TooltipTrigger render={button} />
            <TooltipContent side="bottom">{count > 0 ? `${labels[id]} · ${count}` : labels[id]}</TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}

const EASE = { ease: UI_EASE_OUT } as const;

/**
 * Animates a switch between the section's tabs: the highlight slides to the
 * new tab while its name unfolds and the old tab's folds back into its icon.
 *
 * Everything is measured once, before the first frame paints: where each part
 * is now and where the switch leaves it, header space the switch frees or
 * takes included. Every part then eases there on the same curve, the
 * highlight with them, from where it is to the new tab's final box.
 */
function useTabSwitchMotion(
  listRef: RefObject<HTMLDivElement | null>,
  tab: InboxTab,
  enabled: boolean,
  seconds: number,
  setFolding: (tab: InboxTab | null) => void,
): { x: MotionValue<number>; width: MotionValue<number> } {
  const x = useMotionValue(0);
  const width = useMotionValue(0);
  const shown = useRef<InboxTab | null>(null);
  // The open tab's name as it last settled: where it folds from.
  const openLabelWidth = useRef(0);
  const running = useRef<AnimationPlaybackControls[]>([]);

  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const button = (id: InboxTab) => list.querySelector<HTMLElement>(`[data-inbox-tab="${id}"]`)!;
    const part = (id: InboxTab, name: "icon" | "label") => button(id).querySelector<HTMLElement>(`[data-tab-${name}]`)!;
    if (!enabled) {
      // Motion turned off mid-switch: drop what the stopped animation left.
      shown.current = null;
      for (const el of list.querySelectorAll<HTMLElement>("[data-tab-icon], [data-tab-label]")) {
        el.style.width = "";
        el.style.opacity = "";
        el.querySelector<HTMLElement>(".truncate")?.style.removeProperty("text-overflow");
      }
      return;
    }
    const parts = TABS.flatMap((id) => [part(id, "icon"), part(id, "label")]);
    const box = (el: HTMLElement) => {
      const rect = el.getBoundingClientRect();
      return { x: rect.left - list.getBoundingClientRect().left, w: rect.width };
    };
    const settle = () => {
      for (const el of parts) {
        el.style.width = "";
        el.style.opacity = "";
        el.querySelector<HTMLElement>(".truncate")?.style.removeProperty("text-overflow");
      }
      const target = box(button(tab));
      x.set(target.x);
      width.set(target.w);
      openLabelWidth.current = part(tab, "label").getBoundingClientRect().width;
    };

    const from = shown.current;
    shown.current = tab;

    let cancelled = false;
    if (from !== null && from !== tab && seconds > 0) {
      // Where every part is: mid-switch, its inline style; settled, what the
      // tab it belongs to showed before this switch.
      const start = parts.map((el) => {
        if (el.style.width) return { w: parseFloat(el.style.width), o: parseFloat(el.style.opacity || "1") };
        const id = el.closest<HTMLElement>("[data-inbox-tab]")!.dataset.inboxTab as InboxTab;
        if (el.hasAttribute("data-tab-label")) return id === from ? { w: openLabelWidth.current, o: 1 } : { w: 0, o: 0 };
        return id === from ? { w: 0, o: 0 } : { w: el.firstElementChild!.getBoundingClientRect().width, o: 1 };
      });
      const highlightFrom = { x: x.get(), w: width.get() };

      // Where the switch leaves them, the old tab already holding its place
      // as it will once folded.
      for (const el of parts) {
        el.style.width = "";
        el.style.opacity = "";
      }
      button(from).style.flexShrink = "0";
      const end = parts.map((el) => ({ w: el.getBoundingClientRect().width, o: el.classList.contains("opacity-0") ? 0 : 1 }));
      const toEnd = box(button(tab));
      button(from).style.flexShrink = "";

      parts.forEach((el, i) => {
        el.style.width = `${start[i]!.w}px`;
        el.style.opacity = String(start[i]!.o);
        el.querySelector<HTMLElement>(".truncate")?.style.setProperty("text-overflow", "clip");
      });

      const transition = { duration: seconds, ...EASE };
      running.current = [
        ...parts.map((el, i) =>
          animate(el, { width: [`${start[i]!.w}px`, `${end[i]!.w}px`], opacity: [start[i]!.o, end[i]!.o] }, transition),
        ),
        // Straight to where the new tab ends up, on the parts' curve. Tracking
        // the tabs as they move would bulge past the end when the new tab
        // starts far from where it settles.
        animate(x, [highlightFrom.x, toEnd.x], transition),
        animate(width, [highlightFrom.w, toEnd.w], transition),
      ];
      setFolding(from);
      void Promise.all(running.current).then(() => {
        if (cancelled) return;
        running.current = [];
        setFolding(null);
        settle();
      });
    } else {
      setFolding(null);
      settle();
    }

    // A count arriving, the panel resizing or showing from hidden moves the
    // tabs without a switch.
    const observer = new ResizeObserver(() => {
      if (running.current.length === 0) settle();
    });
    TABS.forEach((id) => observer.observe(button(id)));
    parts.forEach((el) => observer.observe(el));
    return () => {
      cancelled = true;
      observer.disconnect();
      // Stopped where they are: a switch mid-way starts from these inline styles.
      for (const controls of running.current) controls.stop();
      running.current = [];
    };
  }, [listRef, tab, enabled, seconds, setFolding, x, width]);

  return { x, width };
}
