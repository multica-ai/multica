import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { ArrowLeft, ArrowRight, ChevronLeft, ChevronRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useStoryEnv } from "../voice-input/env";
import { countLabel, UI, type Update, type UpdateId } from "./copy";
import { CARD_FRAME, CompactCard, Dots, Hero, UpdateBody, UpdateCard, UpdateContent } from "./card";

const EASE = "cubic-bezier(0.2, 0.8, 0.2, 1)";
const MOVE = `transform 420ms ${EASE}, opacity 260ms ${EASE}, height 320ms ${EASE}`;

export interface OptionProps {
  updates: Update[];
  /** Called when the last card is gone, so the stage can offer a restart. */
  onFinished?: () => void;
  /** Called when a card's call to action is pressed. */
  onCta?: (update: Update) => void;
  /** Arrow keys and Esc drive the cards. Off in the frozen sheets. */
  keyboard?: boolean;
}

/** The corner the star prompt uses: right edge, just above the chat launcher. */
export function Corner({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("absolute right-[var(--chat-launcher-inset)] above-chat-launcher z-50", className)}>{children}</div>
  );
}

function useArrowKeys(enabled: boolean, handlers: { next: () => void; prev: () => void; close: () => void }) {
  const ref = useRef(handlers);
  ref.current = handlers;
  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight") ref.current.next();
      else if (e.key === "ArrowLeft") ref.current.prev();
      else if (e.key === "Escape") ref.current.close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enabled]);
}

function useHeights(count: number) {
  const nodes = useRef<(HTMLDivElement | null)[]>([]);
  const [heights, setHeights] = useState<number[]>([]);
  useLayoutEffect(() => {
    const measure = () => setHeights(nodes.current.slice(0, count).map((n) => n?.offsetHeight ?? 0));
    measure();
    const ro = new ResizeObserver(measure);
    nodes.current.slice(0, count).forEach((n) => n && ro.observe(n));
    return () => ro.disconnect();
  }, [count]);
  return { heights, refAt: (i: number) => (n: HTMLDivElement | null) => void (nodes.current[i] = n) };
}

/* ── A · Deck ─────────────────────────────────────────────────────────── */

function deckStyle(offset: number, height: number | undefined): CSSProperties {
  if (offset < 0) {
    return { transform: "translate(-14%, 28px) rotate(-6deg) scale(0.96)", opacity: 0, zIndex: 40, pointerEvents: "none", height };
  }
  if (offset === 0) return { transform: "none", opacity: 1, zIndex: 30, height };
  const depth = Math.min(offset, 3);
  return {
    transform: `translateY(${-depth * 10}px) scale(${1 - depth * 0.05})`,
    opacity: offset > 2 ? 0 : 1,
    zIndex: 30 - depth,
    height,
  };
}

export function Deck({
  updates,
  onFinished,
  onCta,
  keyboard = false,
  initialIndex = 0,
  onIndexChange,
}: OptionProps & { initialIndex?: number; onIndexChange?: (index: number) => void }) {
  const { lang } = useStoryEnv();
  const [index, setIndex] = useState(initialIndex);
  const [leaving, setLeaving] = useState(false);
  const { heights, refAt } = useHeights(updates.length);
  const frontHeight = heights[index] || undefined;
  const count = updates.length;
  const last = index === count - 1;

  const onIndexChangeRef = useRef(onIndexChange);
  onIndexChangeRef.current = onIndexChange;
  useEffect(() => onIndexChangeRef.current?.(index), [index]);

  const go = (i: number) => setIndex(Math.max(0, Math.min(count - 1, i)));
  const close = () => {
    setLeaving(true);
    window.setTimeout(() => onFinished?.(), 220);
  };
  const next = () => (last ? close() : go(index + 1));
  useArrowKeys(keyboard && !leaving, { next: () => !last && go(index + 1), prev: () => go(index - 1), close });

  return (
    <div
      className="relative w-[23.75rem] animate-in fade-in-0 slide-in-from-bottom-3 duration-300"
      style={{
        height: frontHeight,
        transition: MOVE,
        opacity: leaving ? 0 : 1,
        transform: leaving ? "translateY(12px)" : undefined,
      }}
    >
      {updates.map((update, i) => {
        const offset = i - index;
        const front = offset === 0;
        return (
          <div
            key={update.id}
            inert={!front}
            aria-hidden={!front}
            onClick={offset === 1 ? () => go(i) : undefined}
            className={cn(CARD_FRAME, "absolute right-0 bottom-0 origin-top", offset === 1 && "cursor-pointer")}
            style={{ ...deckStyle(offset, front ? heights[i] || undefined : frontHeight), transition: MOVE }}
          >
            <UpdateContent
              ref={refAt(i)}
              update={update}
              onClose={close}
              footer={
                count === 1 ? (
                  <div className="flex justify-end gap-2">
                    <Button variant="ghost" size="sm" onClick={close}>
                      {UI.done[lang]}
                    </Button>
                    <Button size="sm" onClick={() => (onCta?.(update), close())}>
                      {update.cta[lang]}
                    </Button>
                  </div>
                ) : (
                  <div className="flex items-center justify-between gap-3">
                    <Dots count={count} index={index} onSelect={go} />
                    <div className="flex gap-2">
                      <Button variant="outline" size="sm" onClick={() => (onCta?.(update), next())}>
                        {update.cta[lang]}
                      </Button>
                      <Button size="sm" onClick={next}>
                        {last ? UI.done[lang] : UI.next[lang]}
                        {!last && <ArrowRight data-icon="inline-end" />}
                      </Button>
                    </div>
                  </div>
                )
              }
            />
            {/* Cards further back read as further away. */}
            <div
              className="pointer-events-none absolute inset-0 bg-surface-raised transition-opacity duration-300"
              style={{ opacity: offset > 0 ? Math.min(offset, 2) * 0.28 : 0 }}
            />
          </div>
        );
      })}
    </div>
  );
}

/* ── B · Stack of compact cards ───────────────────────────────────────── */

const ROW = 72;
const GAP = 8;
const HEADER = 32;

export function Stack({
  updates,
  onFinished,
  onCta,
  keyboard = false,
  initialExpanded = false,
  initialOpen = null,
}: OptionProps & { initialExpanded?: boolean; initialOpen?: UpdateId | null }) {
  const { lang } = useStoryEnv();
  const [items, setItems] = useState(updates);
  const [hovered, setHovered] = useState(initialExpanded);
  const [open, setOpen] = useState<UpdateId | null>(initialOpen);
  const expanded = hovered && items.length > 1;

  const dismiss = (id: UpdateId) => {
    const rest = items.filter((u) => u.id !== id);
    setItems(rest);
    if (open === id) setOpen(null);
    if (rest.length === 0) onFinished?.();
  };
  const clearAll = () => {
    setItems([]);
    onFinished?.();
  };
  useArrowKeys(keyboard, { next: () => {}, prev: () => {}, close: () => (open ? setOpen(null) : setHovered(false)) });

  const opened = items.find((u) => u.id === open);
  if (opened) {
    return (
      <div className="animate-in fade-in-0 zoom-in-95 duration-200">
        <UpdateCard
          update={opened}
          onClose={() => dismiss(opened.id)}
          footer={
            <div className="flex items-center justify-between">
              <Button variant="ghost" size="sm" onClick={() => setOpen(null)}>
                <ArrowLeft data-icon="inline-start" />
                {items.length > 1 ? countLabel(items.length, lang) : UI.back[lang]}
              </Button>
              <Button size="sm" onClick={() => (onCta?.(opened), dismiss(opened.id))}>
                {opened.cta[lang]}
              </Button>
            </div>
          }
        />
      </div>
    );
  }
  if (items.length === 0) return null;

  const height = expanded ? HEADER + items.length * ROW + (items.length - 1) * GAP : ROW;
  return (
    <div
      className="relative w-[23.75rem] animate-in fade-in-0 slide-in-from-bottom-3 duration-300"
      style={{ height, transition: MOVE }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocus={() => setHovered(true)}
    >
      <div
        className="absolute inset-x-1 top-0 flex h-7 items-center justify-between text-caption transition-opacity duration-200"
        style={{ opacity: expanded ? 1 : 0, pointerEvents: expanded ? "auto" : "none" }}
      >
        <span className="font-medium text-muted-foreground">{countLabel(items.length, lang)}</span>
        <Button variant="ghost" size="xs" onClick={clearAll}>
          {UI.markAllRead[lang]}
        </Button>
      </div>
      {items.map((update, i) => {
        const depth = Math.min(i, 3);
        const style: CSSProperties = expanded
          ? { transform: `translateY(${-i * (ROW + GAP)}px)`, opacity: 1, zIndex: 30 - i }
          : { transform: `translateY(${-depth * 9}px) scale(${1 - depth * 0.05})`, opacity: i > 2 ? 0 : 1, zIndex: 30 - i };
        return (
          <CompactCard
            key={update.id}
            update={update}
            inert={!expanded && i > 0}
            onOpen={() => setOpen(update.id)}
            onClose={() => dismiss(update.id)}
            className="absolute right-0 bottom-0 origin-top"
            style={{ ...style, transition: MOVE }}
          />
        );
      })}
      {!expanded && items.length > 1 && (
        <span className="pointer-events-none absolute -top-2.5 -left-2 z-40 flex h-5 min-w-5 items-center justify-center rounded-full bg-brand px-1.5 text-micro font-semibold text-brand-foreground shadow-sm">
          {items.length}
        </span>
      )}
    </div>
  );
}

/* ── C · One card that pages ──────────────────────────────────────────── */

export function Carousel({
  updates,
  onFinished,
  onCta,
  keyboard = false,
  initialIndex = 0,
}: OptionProps & { initialIndex?: number }) {
  const { lang } = useStoryEnv();
  const [index, setIndex] = useState(initialIndex);
  const [dir, setDir] = useState<1 | -1>(1);
  const [leaving, setLeaving] = useState(false);
  const count = updates.length;
  const update = updates[index]!;
  const go = (i: number) => {
    if (i < 0 || i >= count || i === index) return;
    setDir(i > index ? 1 : -1);
    setIndex(i);
  };
  const close = () => {
    setLeaving(true);
    window.setTimeout(() => onFinished?.(), 220);
  };
  useArrowKeys(keyboard && !leaving, { next: () => go(index + 1), prev: () => go(index - 1), close });

  return (
    <div
      className="animate-in fade-in-0 slide-in-from-bottom-3 duration-300"
      style={{ transition: MOVE, opacity: leaving ? 0 : 1, transform: leaving ? "translateY(12px)" : undefined }}
    >
      <UpdateCard
        update={update}
        onClose={close}
        hero={
          <div className="overflow-hidden">
            <div className="flex" style={{ transform: `translateX(${-index * 100}%)`, transition: MOVE }}>
              {updates.map((u) => (
                <div key={u.id} className="w-full shrink-0" inert={u.id !== update.id}>
                  <Hero id={u.id} />
                </div>
              ))}
            </div>
          </div>
        }
        body={
          <div
            key={update.id}
            className={cn(
              "animate-in fade-in-0 duration-300",
              dir === 1 ? "slide-in-from-right-3" : "slide-in-from-left-3",
            )}
          >
            <UpdateBody update={update} />
          </div>
        }
        footer={
          <div className="flex items-center justify-between">
            {count > 1 ? (
              <div className="flex items-center gap-0.5">
                <Button variant="ghost" size="icon-xs" aria-label={UI.prev[lang]} disabled={index === 0} onClick={() => go(index - 1)}>
                  <ChevronLeft />
                </Button>
                <span className="min-w-9 text-center text-caption text-muted-foreground tabular-nums">
                  {index + 1} / {count}
                </span>
                <Button variant="ghost" size="icon-xs" aria-label={UI.next[lang]} disabled={index === count - 1} onClick={() => go(index + 1)}>
                  <ChevronRight />
                </Button>
              </div>
            ) : (
              <span />
            )}
            <Button size="sm" onClick={() => onCta?.(update)}>
              {update.cta[lang]}
            </Button>
          </div>
        }
      />
    </div>
  );
}
