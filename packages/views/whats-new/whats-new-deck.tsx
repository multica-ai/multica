"use client";

import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
} from "react";
import { ArrowRight, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { needsSourceBackfill } from "@multica/core/onboarding";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useSessionPromptStore } from "@multica/core/session-prompt";
import { isFloatingChatRouteSuppressed } from "../chat/floating-chat-visibility";
import { isQuietMoment } from "../common/quiet-moment";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { readSourceBackfillDismissCount } from "../onboarding/source-backfill-dismiss";
import { HighlightActionButton } from "./highlight-action";
import { HeroFrame, type WhatsNewHero } from "./heroes";
import { highlightCopy, type WhatsNewHighlight, type WhatsNewRelease } from "./releases";
import { useWhatsNew } from "./use-whats-new";
import { formatReleaseDate, versionLabel } from "./whats-new-panel";

/** Long enough for the workspace to render and for the source survey (700ms) to claim the session first. */
export const WHATS_NEW_DECK_DELAY_MS = 3000;
/** How often a busy moment (typing, a dialog, the chat window) is checked again. */
export const WHATS_NEW_DECK_RETRY_MS = 5000;
export const WHATS_NEW_DECK_MAX_CARDS = 5;

const LEAVE_MS = 220;
const EASE = "cubic-bezier(0.2, 0.8, 0.2, 1)";
const MOVE = `transform 420ms ${EASE}, opacity 260ms ${EASE}, height 320ms ${EASE}`;
interface DeckCard {
  release: WhatsNewRelease;
  highlight: WhatsNewHighlight;
  hero: WhatsNewHero;
}

/** Only highlights with hero art are dealt; the rest stay in the What's new panel. */
function deckCards(unread: readonly WhatsNewRelease[]): DeckCard[] {
  return unread
    .flatMap((release) =>
      release.highlights.flatMap((highlight) => (highlight.hero ? [{ release, highlight, hero: highlight.hero }] : [])),
    )
    .slice(0, WHATS_NEW_DECK_MAX_CARDS);
}

/**
 * Unread release highlights as a deck of cards in the star prompt's corner.
 * Opens once per app session when nothing else is happening; closing it marks
 * the release seen, and the highlights stay in What's new in the Help menu.
 */
export function WhatsNewDeck() {
  // Shells mount this while the workspace slug is still resolving.
  const wsId = useCurrentWorkspace()?.id ?? null;
  const { unread, markSeen } = useWhatsNew();
  const chatOpen = useChatStore((s) => s.isOpen);
  const floatingChatEnabled = useChatStore((s) => s.floatingChatEnabled);
  const { pathname } = useNavigation();
  const chatPath = useWorkspacePaths().chat();
  const launcherVisible =
    floatingChatEnabled && !chatOpen && !isFloatingChatRouteSuppressed(pathname, chatPath);
  const [cards, setCards] = useState<DeckCard[] | null>(null);
  const [leaving, setLeaving] = useState(false);

  const hasUnread = unread.length > 0;
  const dealable = useMemo(() => deckCards(unread), [unread]);
  const dealableRef = useRef(dealable);
  dealableRef.current = dealable;
  const hasDealable = dealable.length > 0;

  useEffect(() => {
    if (cards || !wsId || !hasDealable) return;
    let timer: number | undefined;
    const consider = () => {
      if (useSessionPromptStore.getState().prompted) return;
      const user = useAuthStore.getState().user;
      // The attribution survey owns this session's one interruption.
      if (needsSourceBackfill(user, readSourceBackfillDismissCount(user?.id))) return;
      if (useChatStore.getState().isOpen || !isQuietMoment()) {
        timer = window.setTimeout(consider, WHATS_NEW_DECK_RETRY_MS);
        return;
      }
      useSessionPromptStore.getState().markPrompted();
      setCards(dealableRef.current);
    };
    timer = window.setTimeout(consider, WHATS_NEW_DECK_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [cards, wsId, hasDealable]);

  useEffect(() => {
    if (!leaving) return;
    const timer = window.setTimeout(() => {
      setCards(null);
      setLeaving(false);
    }, LEAVE_MS);
    return () => window.clearTimeout(timer);
  }, [leaving]);

  // Opening What's new from the Help menu also marks the release seen.
  if (!cards || (!hasUnread && !leaving)) return null;

  const finish = () => {
    markSeen();
    setLeaving(true);
  };

  return (
    <Deck
      cards={cards}
      leaving={leaving}
      // The chat window covers this corner while it is open; the deck keeps its place behind it.
      hidden={chatOpen}
      onFinish={finish}
      className={launcherVisible ? "above-chat-launcher" : "bottom-[var(--chat-launcher-inset)]"}
    />
  );
}

function deckStyle(offset: number, height: number | undefined): CSSProperties {
  if (offset < 0) {
    return { transform: "translate(-14%, 28px) rotate(-6deg) scale(0.96)", opacity: 0, zIndex: 40, height };
  }
  if (offset === 0) return { transform: "none", opacity: 1, zIndex: 30, height };
  const depth = Math.min(offset, 3);
  return {
    transform: `translateY(${-depth * 10}px) scale(${1 - depth * 0.05})`,
    // Two cards peek out behind the front one; the rest wait out of sight.
    opacity: offset > 2 ? 0 : 1,
    zIndex: 30 - depth,
    height,
  };
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

function Deck({
  cards,
  leaving,
  hidden,
  onFinish,
  className,
}: {
  cards: DeckCard[];
  leaving: boolean;
  hidden: boolean;
  onFinish: () => void;
  className: string;
}) {
  const { t } = useT("layout");
  const [index, setIndex] = useState(0);
  const { heights, refAt } = useHeights(cards.length);
  const deckRef = useRef<HTMLElement>(null);
  const primaryRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const refocus = useRef(false);
  const count = cards.length;
  const last = index === count - 1;
  const frontHeight = heights[index] || undefined;

  // The pressed button goes inert with its card; hand focus to the new front card.
  useEffect(() => {
    if (!refocus.current) return;
    refocus.current = false;
    primaryRefs.current[index]?.focus({ preventScroll: true });
  }, [index]);

  const go = (i: number) => {
    const target = Math.max(0, Math.min(count - 1, i));
    if (target === index) return;
    refocus.current = deckRef.current?.contains(document.activeElement) ?? false;
    setIndex(target);
  };
  const next = () => (last ? onFinish() : go(index + 1));

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "ArrowRight") go(index + 1);
    else if (e.key === "ArrowLeft") go(index - 1);
    else if (e.key === "Escape") onFinish();
    else return;
    e.preventDefault();
  };

  return (
    <section
      ref={deckRef}
      aria-label={t(($) => $.whats_new.trigger)}
      data-slot="whats-new-deck"
      hidden={hidden}
      onKeyDown={onKeyDown}
      className={cn(
        "absolute right-[var(--chat-launcher-inset)] z-50 w-[min(23.75rem,calc(100%-2*var(--chat-launcher-inset)))]",
        "animate-in fade-in-0 slide-in-from-bottom-3 duration-300 motion-reduce:animate-none",
        className,
      )}
      style={{
        height: frontHeight,
        transition: MOVE,
        opacity: leaving ? 0 : 1,
        transform: leaving ? "translateY(12px)" : undefined,
      }}
    >
      {cards.map((card, i) => {
        const offset = i - index;
        return (
          <div
            key={`${card.release.version}:${card.highlight.id}`}
            className="absolute right-0 bottom-0 w-full origin-top overflow-hidden rounded-xl bg-surface-raised text-popover-foreground shadow-[var(--floating-shadow)] ring-1 ring-floating-border motion-reduce:!transition-none"
            style={{
              ...deckStyle(offset, offset === 0 ? heights[i] || undefined : frontHeight),
              transition: MOVE,
              pointerEvents: offset < 0 ? "none" : undefined,
            }}
          >
            <UpdateCard
              ref={refAt(i)}
              card={card}
              front={offset === 0}
              count={count}
              index={index}
              primaryRef={(n) => void (primaryRefs.current[i] = n)}
              onNext={next}
              onSelect={go}
              onFinish={onFinish}
            />
            {/* Cards further back read as further away; the first one back advances the deck. */}
            {offset > 0 && (
              <div
                aria-hidden="true"
                onClick={offset === 1 ? () => go(i) : undefined}
                className={cn(
                  "absolute inset-0 bg-surface-raised transition-opacity duration-300",
                  offset === 1 && "cursor-pointer",
                )}
                style={{ opacity: Math.min(offset, 2) * 0.28 }}
              />
            )}
          </div>
        );
      })}
    </section>
  );
}

function UpdateCard({
  ref,
  card,
  front,
  count,
  index,
  primaryRef,
  onNext,
  onSelect,
  onFinish,
}: {
  ref: (n: HTMLDivElement | null) => void;
  card: DeckCard;
  front: boolean;
  count: number;
  index: number;
  primaryRef: (n: HTMLButtonElement | null) => void;
  onNext: () => void;
  onSelect: (i: number) => void;
  onFinish: () => void;
}) {
  const { t, i18n } = useT("layout");
  const titleId = useId();
  const { release, highlight, hero } = card;
  const copy = highlightCopy(highlight, i18n.language);
  const last = index === count - 1;

  return (
    <div ref={ref} inert={!front} aria-hidden={!front} role="group" aria-labelledby={titleId}>
      <div className="relative">
        <HeroFrame hero={hero} />
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={t(($) => $.whats_new.panel.close)}
          onClick={onFinish}
          className="absolute top-2.5 right-2.5 bg-surface-raised/90 text-muted-foreground shadow-sm hover:bg-surface-raised"
        >
          <X />
        </Button>
      </div>
      <div className="px-5 pt-4 pb-4">
        <div className="flex items-center gap-2 text-micro">
          <span className="inline-flex h-[18px] items-center rounded-xs bg-brand/10 px-1.5 font-medium text-brand">
            {t(($) => $.whats_new.deck.tag)}
          </span>
          <span className="text-muted-foreground tabular-nums">
            {versionLabel(release.version)} · {formatReleaseDate(release.date, i18n.language)}
          </span>
        </div>
        <h2 id={titleId} className="mt-2 text-body-lg font-semibold [overflow-wrap:anywhere]">
          {copy.title}
        </h2>
        <p className="mt-1 text-label text-muted-foreground">{copy.body}</p>
        <div className="mt-4 flex items-center gap-3">
          {count > 1 && <Dots count={count} index={index} onSelect={onSelect} />}
          <div className="ml-auto flex items-center gap-3">
            {highlight.action && <HighlightActionButton action={highlight.action} onDone={onFinish} />}
            <Button ref={primaryRef} size="sm" onClick={onNext}>
              {last ? t(($) => $.whats_new.deck.done) : t(($) => $.whats_new.deck.next)}
              {!last && <ArrowRight data-icon="inline-end" />}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function Dots({ count, index, onSelect }: { count: number; index: number; onSelect: (i: number) => void }) {
  const { t } = useT("layout");
  return (
    <div className="flex items-center gap-1">
      {Array.from({ length: count }, (_, i) => (
        <button
          key={i}
          type="button"
          aria-label={t(($) => $.whats_new.deck.page, { current: i + 1, total: count })}
          aria-current={i === index ? "step" : undefined}
          onClick={() => onSelect(i)}
          className={cn(
            // eslint-disable-next-line no-restricted-syntax -- page dots
            "h-1.5 cursor-pointer rounded-full outline-none transition-[width,background-color] duration-200 focus-visible:ring-2 focus-visible:ring-ring/50",
            i === index ? "w-4 bg-foreground" : "w-1.5 bg-muted-foreground/30 hover:bg-muted-foreground/60",
          )}
        />
      ))}
    </div>
  );
}
