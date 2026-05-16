"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  Archive,
  ArchiveRestore,
  BellOff,
  BellRing,
  MailOpen,
  MailWarning,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
} from "@multica/ui/components/ui/drawer";
import { useMarkInboxRead } from "@multica/core/inbox/mutations";
import type { InboxItem } from "@multica/core/types";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useMuteInbox, useUnmuteInbox, useMarkInboxUnread } from "../mutations";
import { isMuted, nextLocalEightAm } from "../mute-time";
import { useCerebroInboxStrings } from "../strings";
import {
  ARCHIVE_COMMIT_DELAY_MS,
  DIRECTION_DECIDE_PX,
  LEFT_PANEL_REVEAL_PX,
  LONG_PRESS_MS,
  POST_SWIPE_CLICK_SUPPRESS_MS,
  SWIPE_INTENT_PX,
  SWIPE_ROW_TRANSITION_MS,
  commitThresholdPx,
} from "../swipe-thresholds";

interface Props {
  item: InboxItem;
  /**
   * Existing archive callback owned by the upstream row. We invoke it for
   * the swipe-right gesture and the menu's "Arkiv" item rather than running
   * our own archive mutation, so batch-archive paths stay consistent.
   */
  onArchive: () => void;
}

/**
 * Cerebro row-actions surface around an inbox item. Renders:
 *
 * - Desktop hover: archive + "..." icons (positioned absolute right)
 * - Desktop right-click: same menu as "..."
 * - Mobile swipe right (≥80px): triggers archive on release
 * - Mobile swipe left (≥144px): reveals a two-button strip (læst/ulæst, mute)
 *   that stays open until tap-outside or action selected
 * - Mobile long-press (≥500ms): opens a bottom drawer with all actions
 *
 * The component is mounted as a child of the upstream row. On mobile gestures
 * it manipulates its parent button via DOM transforms so the row visibly
 * follows the finger.
 */
export function CerebroInboxRowActions({ item, onArchive }: Props) {
  // All hooks must be called unconditionally before any early return, so the
  // feature-flag toggle doesn't change the hook count between renders.
  const enabled = useFeatureFlag("cerebro_inbox_row_actions");
  const isMobile = useIsMobile();
  const markRead = useMarkInboxRead();
  const markUnread = useMarkInboxUnread();
  const mute = useMuteInbox();
  const unmute = useUnmuteInbox();
  const strings = useCerebroInboxStrings();
  const muted = isMuted(item.muted_until);

  const handleToggleRead = useCallback(() => {
    if (item.read) markUnread.mutate(item.id);
    else markRead.mutate(item.id);
  }, [item.id, item.read, markRead, markUnread]);

  const handleToggleMute = useCallback(() => {
    if (muted) unmute.mutate(item.id);
    else mute.mutate({ id: item.id, mutedUntil: nextLocalEightAm() });
  }, [item.id, muted, mute, unmute]);

  // Feature-flag off — render the simple hover-only archive button so the
  // user still has the upstream archive UX. Mirrors the icon used in the
  // upstream InboxListItemShell pre-cerebro.
  if (!enabled) return <SimpleArchiveButton onArchive={onArchive} />;

  const menuItems = (
    <MenuItems
      item={item}
      muted={muted}
      strings={strings}
      onToggleRead={handleToggleRead}
      onToggleMute={handleToggleMute}
      onArchive={onArchive}
    />
  );

  if (isMobile) {
    return (
      <MobileRowActions
        muted={muted}
        strings={strings}
        onArchive={onArchive}
        onToggleRead={handleToggleRead}
        onToggleMute={handleToggleMute}
        unread={!item.read}
        renderDrawerMenu={(close) => (
          // The long-press drawer is NOT a Menu.Root — Base UI's
          // `DropdownMenuItem` (Menu.Item) silently no-ops onClick when
          // rendered outside a menu context, which caused mute / mark-unread
          // to feel "dead" when triggered from the drawer. Render plain
          // buttons here instead and close the drawer on selection.
          <DrawerActionList
            item={item}
            muted={muted}
            strings={strings}
            onToggleRead={() => {
              close();
              handleToggleRead();
            }}
            onToggleMute={() => {
              close();
              handleToggleMute();
            }}
            onArchive={() => {
              close();
              onArchive();
            }}
          />
        )}
      />
    );
  }
  return (
    <DesktopRowActions
      strings={strings}
      menuItems={menuItems}
      onArchive={onArchive}
      onToggleRead={handleToggleRead}
      onToggleMute={handleToggleMute}
      muted={muted}
      unread={!item.read}
    />
  );
}

/* -------------------------------------------------------------------------- */
/*  Desktop                                                                    */
/* -------------------------------------------------------------------------- */

interface DesktopProps {
  menuItems: ReactNode;
  onArchive: () => void;
  onToggleRead: () => void;
  onToggleMute: () => void;
  muted: boolean;
  unread: boolean;
  strings: ReturnType<typeof useCerebroInboxStrings>;
}

function DesktopRowActions({
  menuItems,
  onArchive,
  onToggleRead,
  onToggleMute,
  muted,
  unread,
  strings,
}: DesktopProps) {
  // JEH-663 — mark-unread + mute are surfaced as direct hover icons (next to
  // archive) so the user reaches them in one click instead of going through
  // a "..." sub-menu. The "..." dropdown trigger has been removed; the
  // right-click menu stays as a quick keyboard-free fallback that anchors at
  // the click point (Linear/Gmail style).
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [contextMenuOpen, setContextMenuOpen] = useState(false);
  const [contextPos, setContextPos] = useState<{ x: number; y: number }>({
    x: 0,
    y: 0,
  });

  useEffect(() => {
    const node = wrapperRef.current?.parentElement;
    if (!node) return;
    const handler = (e: MouseEvent) => {
      e.preventDefault();
      setContextPos({ x: e.clientX, y: e.clientY });
      setContextMenuOpen(true);
    };
    node.addEventListener("contextmenu", handler);
    return () => node.removeEventListener("contextmenu", handler);
  }, []);

  return (
    <div ref={wrapperRef}>
      {/* Hover-only icon group: positioned absolute over the row, only shown
          when the parent row is hovered. Three icons left-to-right: mark
          unread/read, mute/unmute, archive. */}
      <div
        className="absolute right-2 top-1/2 -translate-y-1/2 hidden items-center gap-1 sm:group-hover:flex"
        onClick={(e) => e.stopPropagation()}
      >
        <IconButton
          aria-label={unread ? strings.mark_read_tooltip : strings.mark_unread_tooltip}
          onClick={onToggleRead}
          title={unread ? strings.mark_read_tooltip : strings.mark_unread_tooltip}
        >
          {unread ? (
            <MailOpen className="size-3.5" />
          ) : (
            <MailWarning className="size-3.5" />
          )}
        </IconButton>
        <IconButton
          aria-label={muted ? strings.unmute_tooltip : strings.mute_tooltip}
          onClick={onToggleMute}
          title={muted ? strings.unmute_tooltip : strings.mute_tooltip}
        >
          {muted ? <BellRing className="size-3.5" /> : <BellOff className="size-3.5" />}
        </IconButton>
        <IconButton
          aria-label={strings.archive_label}
          onClick={onArchive}
          title={strings.archive_tooltip}
        >
          <Archive className="size-3.5" />
        </IconButton>
      </div>

      {/* Right-click menu: phantom 1×1 anchor at the click point lives outside
          the hover-only group so it stays measurable even before the user
          hovers the row. */}
      <DropdownMenu open={contextMenuOpen} onOpenChange={setContextMenuOpen}>
        <DropdownMenuTrigger
          render={(props) => (
            <span
              {...props}
              aria-hidden
              className="pointer-events-none fixed size-px"
              style={{ left: contextPos.x, top: contextPos.y }}
            />
          )}
        />
        <DropdownMenuContent align="start" className="min-w-44">
          {menuItems}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Mobile                                                                     */
/* -------------------------------------------------------------------------- */

interface MobileProps {
  muted: boolean;
  unread: boolean;
  onArchive: () => void;
  onToggleRead: () => void;
  onToggleMute: () => void;
  renderDrawerMenu: (close: () => void) => ReactNode;
  strings: ReturnType<typeof useCerebroInboxStrings>;
}

function MobileRowActions({
  muted,
  unread,
  onArchive,
  onToggleRead,
  onToggleMute,
  renderDrawerMenu,
  strings,
}: MobileProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  // offsetX is mirrored in a ref so the touchend handler reads the live
  // gesture distance synchronously without waiting for a React render.
  const offsetXRef = useRef(0);
  const [offsetX, setOffsetXState] = useState(0);
  const setOffsetX = useCallback((v: number) => {
    offsetXRef.current = v;
    setOffsetXState(v);
  }, []);
  const [revealed, setRevealed] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const archiveCommitTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Tracks whether the gesture is still in flight so the transform effect
  // can decide whether to apply a snap-back transition on release.
  const gestureActiveRef = useRef(false);
  // Timestamp (ms-since-epoch) of the most recent touchend with meaningful
  // horizontal motion. The parent shell's capture-phase click listener
  // suppresses clicks that arrive within POST_SWIPE_CLICK_SUPPRESS_MS of
  // this stamp, so the synthetic click iOS Safari sometimes fires after a
  // swipe doesn't navigate the user into the issue. Using a timestamp
  // instead of a boolean is critical: when the gesture has enough movement
  // to count as a drag, iOS Safari does NOT fire a synthetic click at all,
  // so a boolean flag never gets reset by the swallow path — it stays
  // stuck-true and eats the user's NEXT tap on the reveal panel.
  const swipeEndedAtRef = useRef(0);
  // Ref to the revealed left-panel div so the capture-phase click suppressor
  // can exempt taps that land inside it — the panel's own onClick handlers
  // already call stopPropagation, but the capture handler runs first and was
  // eating the first legitimate tap after a swipe-left.
  const leftPanelRef = useRef<HTMLDivElement>(null);

  // Stable refs for callbacks so the touch-event effect doesn't have to
  // re-attach listeners on every render — listener re-attachment in the
  // middle of a gesture would lose the in-flight closure state.
  const onArchiveRef = useRef(onArchive);
  const onDrawerOpenRef = useRef(setDrawerOpen);
  useEffect(() => {
    onArchiveRef.current = onArchive;
    onDrawerOpenRef.current = setDrawerOpen;
  });

  useEffect(
    () => () => {
      if (archiveCommitTimeoutRef.current !== null) {
        clearTimeout(archiveCommitTimeoutRef.current);
      }
    },
    [],
  );

  // Apply the offset transform to the parent row so the user sees the row
  // visually follow the finger. Snap-back animates only when the gesture
  // has ended (gestureActiveRef = false).
  useEffect(() => {
    const row = containerRef.current?.parentElement;
    if (!row) return;
    if (offsetX === 0 && !revealed) {
      row.style.transform = "";
    } else {
      const x = revealed && offsetX === 0 ? -LEFT_PANEL_REVEAL_PX : offsetX;
      row.style.transform = `translateX(${x}px)`;
    }
    row.style.transition = gestureActiveRef.current ? "" : `transform ${SWIPE_ROW_TRANSITION_MS}ms`;
  }, [offsetX, revealed]);

  // Native TouchEvent listeners with `passive: false` so we can call
  // `preventDefault()` on touchmove once the gesture is horizontal. Without
  // this, iOS Safari hands the gesture off to native scroll mid-swipe and
  // pointer/touch events stop firing — the "on/off" symptom in v3.
  useEffect(() => {
    const overlay = containerRef.current;
    if (!overlay) return;

    let startX = 0;
    let startY = 0;
    let direction: "horizontal" | "vertical" | null = null;
    let active = false;
    let longPressId: ReturnType<typeof setTimeout> | null = null;

    const cancelLong = () => {
      if (longPressId !== null) {
        clearTimeout(longPressId);
        longPressId = null;
      }
    };

    const onTouchStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return;
      const t = e.touches[0]!;
      startX = t.clientX;
      startY = t.clientY;
      direction = null;
      active = true;
      gestureActiveRef.current = true;
      longPressId = setTimeout(() => {
        onDrawerOpenRef.current(true);
        active = false;
        gestureActiveRef.current = false;
      }, LONG_PRESS_MS);
    };

    const onTouchMove = (e: TouchEvent) => {
      if (!active || e.touches.length !== 1) return;
      const t = e.touches[0]!;
      const dx = t.clientX - startX;
      const dy = t.clientY - startY;
      const adx = Math.abs(dx);
      const ady = Math.abs(dy);

      if (direction === null) {
        if (adx < DIRECTION_DECIDE_PX && ady < DIRECTION_DECIDE_PX) return;
        // Pick the dominant axis. Whichever has the larger delta wins —
        // simpler and more inclusive than the v3 1.5× ratio, which made
        // natural-arc swipes occasionally lock to vertical.
        direction = adx > ady ? "horizontal" : "vertical";
        cancelLong();
        if (direction === "vertical") {
          active = false;
          gestureActiveRef.current = false;
          return;
        }
      }
      if (direction !== "horizontal") return;

      // Critical: claim the gesture so the browser doesn't take it for
      // native vertical scroll. Listener is registered with passive:false
      // (see addEventListener call below) so this actually works.
      e.preventDefault();
      offsetXRef.current = dx;
      setOffsetX(dx);
    };

    const onTouchEnd = () => {
      cancelLong();
      const wasHorizontal = direction === "horizontal";
      direction = null;
      active = false;
      gestureActiveRef.current = false;
      if (!wasHorizontal) return;

      const live = offsetXRef.current;
      const rowWidth = overlay.parentElement?.clientWidth ?? 360;
      const commitPx = commitThresholdPx(rowWidth);

      // Suppress the synthetic click after any meaningful horizontal
      // motion, even if the swipe didn't reach the commit threshold —
      // otherwise a partial swipe that springs back navigates the user
      // into the issue, which feels like the gesture didn't work at all.
      if (Math.abs(live) > SWIPE_INTENT_PX) {
        swipeEndedAtRef.current = Date.now();
      }

      if (live >= commitPx) {
        setRevealed(false);
        archiveCommitTimeoutRef.current = setTimeout(() => {
          setOffsetX(0);
          onArchiveRef.current();
          archiveCommitTimeoutRef.current = null;
        }, ARCHIVE_COMMIT_DELAY_MS);
      } else if (live <= -commitPx) {
        setOffsetX(0);
        setRevealed(true);
      } else {
        setOffsetX(0);
      }
    };

    overlay.addEventListener("touchstart", onTouchStart, { passive: false });
    overlay.addEventListener("touchmove", onTouchMove, { passive: false });
    overlay.addEventListener("touchend", onTouchEnd);
    overlay.addEventListener("touchcancel", onTouchEnd);
    return () => {
      cancelLong();
      overlay.removeEventListener("touchstart", onTouchStart);
      overlay.removeEventListener("touchmove", onTouchMove);
      overlay.removeEventListener("touchend", onTouchEnd);
      overlay.removeEventListener("touchcancel", onTouchEnd);
    };
  }, [setOffsetX]);

  // Capture-phase click listener on the parent row shell: when a
  // meaningful swipe just happened, the synthetic click iOS Safari
  // sometimes fires after touchend is intercepted here so the row's
  // onClick (navigation) doesn't run on a swipe attempt. The timestamp
  // window auto-expires, so a legitimate tap on the reveal panel that
  // arrives later still goes through.
  useEffect(() => {
    const node = containerRef.current?.parentElement;
    if (!node) return;
    const handler = (e: MouseEvent) => {
      if (Date.now() - swipeEndedAtRef.current < POST_SWIPE_CLICK_SUPPRESS_MS) {
        // Taps inside the revealed action panel are intentional — the panel's
        // own onClick handlers call stopPropagation, but capture runs first.
        // Exempt them so the first tap after swipe-left isn't eaten.
        if (leftPanelRef.current?.contains(e.target as Node)) return;
        swipeEndedAtRef.current = 0;
        e.stopPropagation();
        e.preventDefault();
      }
    };
    node.addEventListener("click", handler, true);
    return () => node.removeEventListener("click", handler, true);
  }, []);

  const closePanel = useCallback(() => {
    setRevealed(false);
    setOffsetX(0);
  }, [setOffsetX]);

  // Tap-outside closes the revealed panel.
  useEffect(() => {
    if (!revealed) return;
    const handler = (e: TouchEvent | MouseEvent) => {
      const row = containerRef.current?.parentElement;
      if (!row) return;
      if (!row.contains(e.target as Node)) closePanel();
    };
    document.addEventListener("touchstart", handler, { passive: true });
    document.addEventListener("mousedown", handler);
    return () => {
      document.removeEventListener("touchstart", handler);
      document.removeEventListener("mousedown", handler);
    };
  }, [revealed, closePanel]);

  // Swipe-right reveal background (archive cue).
  const archiveBg = offsetX > 0 && (
    <div
      className="absolute inset-y-0 left-0 flex items-center justify-start gap-2 bg-destructive px-4 text-white"
      style={{ width: Math.max(offsetX, 0) }}
      aria-hidden
    >
      <Archive className="size-4" />
      <span className="text-xs font-medium text-white">{strings.swipe_archive}</span>
    </div>
  );

  // Swipe-left action panel.
  const leftPanel = (offsetX < 0 || revealed) && (
    <div
      ref={leftPanelRef}
      className="absolute inset-y-0 right-0 flex items-stretch"
      style={{ width: revealed ? LEFT_PANEL_REVEAL_PX : Math.max(-offsetX, 0) }}
      aria-hidden={!revealed}
    >
      <span
        role="button"
        tabIndex={-1}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          closePanel();
          onToggleRead();
        }}
        className="flex flex-1 flex-col items-center justify-center gap-1 bg-brand px-1 text-center text-xs font-medium leading-tight text-brand-foreground"
      >
        {unread ? (
          <MailOpen className="size-4" />
        ) : (
          <MailWarning className="size-4" />
        )}
        <span>{unread ? strings.mark_read : strings.mark_unread}</span>
      </span>
      <span
        role="button"
        tabIndex={-1}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          closePanel();
          onToggleMute();
        }}
        className="flex flex-1 flex-col items-center justify-center gap-1 bg-amber-500 px-1 text-center text-xs font-medium leading-tight text-white"
      >
        {muted ? <BellRing className="size-4" /> : <BellOff className="size-4" />}
        <span>{muted ? strings.unmute : strings.mute}</span>
      </span>
    </div>
  );

  return (
    <>
      <div
        ref={containerRef}
        className="absolute inset-0 sm:hidden"
        // touch-action: pan-y lets vertical scroll work natively, while we
        // claim horizontal gestures via preventDefault in the touchmove
        // listener attached in useEffect above.
        style={{ touchAction: "pan-y" }}
      />
      {archiveBg}
      {leftPanel}
      <Drawer open={drawerOpen} onOpenChange={setDrawerOpen}>
        <DrawerContent>
          <DrawerHeader>
            <DrawerTitle>{strings.drawer_title}</DrawerTitle>
          </DrawerHeader>
          <div className="px-4 pb-6">
            {renderDrawerMenu(() => setDrawerOpen(false))}
          </div>
        </DrawerContent>
      </Drawer>
    </>
  );
}

/* -------------------------------------------------------------------------- */
/*  Shared menu items                                                          */
/* -------------------------------------------------------------------------- */

function MenuItems({
  item,
  muted,
  strings,
  onToggleRead,
  onToggleMute,
  onArchive,
}: {
  item: InboxItem;
  muted: boolean;
  strings: ReturnType<typeof useCerebroInboxStrings>;
  onToggleRead: () => void;
  onToggleMute: () => void;
  onArchive: () => void;
}) {
  // Same items rendered inside DropdownMenu, ContextMenu, and Drawer — the
  // primitives all accept the shadcn DropdownMenuItem children since their
  // sub-components share the Base UI Menu primitive.
  return (
    <>
      <DropdownMenuItem onClick={onToggleRead}>
        {item.read ? (
          <MailWarning className="size-4" />
        ) : (
          <MailOpen className="size-4" />
        )}
        {item.read ? strings.mark_unread : strings.mark_read}
      </DropdownMenuItem>
      <DropdownMenuItem onClick={onToggleMute}>
        {muted ? <BellRing className="size-4" /> : <BellOff className="size-4" />}
        {muted ? strings.unmute : strings.mute}
      </DropdownMenuItem>
      <DropdownMenuItem onClick={onArchive}>
        <Archive className="size-4" />
        {strings.archive_label}
      </DropdownMenuItem>
    </>
  );
}

/**
 * Drawer-friendly variant of MenuItems. Uses plain buttons instead of
 * DropdownMenuItem (which is Base UI Menu.Item — silently no-ops outside a
 * menu context, which is why mute / mark-unread "did nothing" when the
 * long-press drawer was the entry point).
 */
function DrawerActionList({
  item,
  muted,
  strings,
  onToggleRead,
  onToggleMute,
  onArchive,
}: {
  item: InboxItem;
  muted: boolean;
  strings: ReturnType<typeof useCerebroInboxStrings>;
  onToggleRead: () => void;
  onToggleMute: () => void;
  onArchive: () => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <DrawerActionButton onClick={onToggleRead}>
        {item.read ? (
          <MailWarning className="size-5" />
        ) : (
          <MailOpen className="size-5" />
        )}
        <span>{item.read ? strings.mark_unread : strings.mark_read}</span>
      </DrawerActionButton>
      <DrawerActionButton onClick={onToggleMute}>
        {muted ? <BellRing className="size-5" /> : <BellOff className="size-5" />}
        <span>{muted ? strings.unmute : strings.mute}</span>
      </DrawerActionButton>
      <DrawerActionButton onClick={onArchive}>
        <Archive className="size-5" />
        <span>{strings.archive_label}</span>
      </DrawerActionButton>
    </div>
  );
}

function DrawerActionButton({
  onClick,
  children,
}: {
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-3 rounded-md px-3 py-3 text-left text-base hover:bg-accent active:bg-accent"
    >
      {children}
    </button>
  );
}

/* -------------------------------------------------------------------------- */
/*  Helpers                                                                    */
/* -------------------------------------------------------------------------- */

function IconButton({
  children,
  className,
  onClick,
  ...props
}: React.HTMLAttributes<HTMLSpanElement>) {
  // role=button span (not <button>) keeps this safe inside row-level click
  // surfaces across desktop and mobile renderers.
  return (
    <span
      role="button"
      tabIndex={-1}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onClick?.(e);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.stopPropagation();
          (onClick as ((e: React.SyntheticEvent) => void) | undefined)?.(e);
        }
      }}
      className={cn(
        "flex h-7 w-7 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground",
        className,
      )}
      {...props}
    >
      {children}
    </span>
  );
}

/**
 * Swipe-only cerebro variant for inbox rows that aren't issue items —
 * channels, DMs, chat sessions. The full row-actions surface is item-
 * specific (mute / mark-unread keyed off `inbox_item.id`), but archive
 * applies uniformly to any row in the inbox queue, so this slimmer
 * component gives those rows the same swipe-right ergonomics without
 * dragging in the rest of the menu.
 *
 * Renders:
 * - Mobile: full-row touch overlay; swipe-right past commit threshold
 *   archives on release.
 * - Desktop: hover-only archive icon by default. Pass `hideOnDesktop`
 *   when the host row already has its own archive button (e.g. chat
 *   sessions in the inbox sidebar) — we render `null` to avoid two
 *   archive icons stacking.
 */
export function CerebroSwipeArchive({
  onArchive,
  hideOnDesktop,
}: {
  onArchive: () => void;
  /** Skip the desktop hover-icon fallback. Use when the host row already
   *  renders its own archive control on desktop. */
  hideOnDesktop?: boolean;
}) {
  const enabled = useFeatureFlag("cerebro_inbox_row_actions");
  const isMobile = useIsMobile();
  const strings = useCerebroInboxStrings();

  if (!enabled || !isMobile) {
    if (hideOnDesktop) return null;
    return <SimpleArchiveButton onArchive={onArchive} />;
  }

  return <SwipeArchiveOnly onArchive={onArchive} strings={strings} />;
}

function SwipeArchiveOnly({
  onArchive,
  strings,
}: {
  onArchive: () => void;
  strings: ReturnType<typeof useCerebroInboxStrings>;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const offsetXRef = useRef(0);
  const [offsetX, setOffsetXState] = useState(0);
  const setOffsetX = useCallback((v: number) => {
    offsetXRef.current = v;
    setOffsetXState(v);
  }, []);
  const gestureActiveRef = useRef(false);
  // Timestamp of the most recent meaningful swipe; see MobileRowActions
  // for the full rationale on why this is a timestamp, not a boolean.
  const swipeEndedAtRef = useRef(0);
  const archiveCommitTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Stable ref for the archive callback so the touch effect doesn't re-
  // attach listeners every render.
  const onArchiveRef = useRef(onArchive);
  useEffect(() => {
    onArchiveRef.current = onArchive;
  });

  useEffect(
    () => () => {
      if (archiveCommitTimeoutRef.current !== null) {
        clearTimeout(archiveCommitTimeoutRef.current);
      }
    },
    [],
  );

  useEffect(() => {
    const row = containerRef.current?.parentElement;
    if (!row) return;
    if (offsetX === 0) {
      row.style.transform = "";
    } else {
      row.style.transform = `translateX(${offsetX}px)`;
    }
    row.style.transition = gestureActiveRef.current ? "" : `transform ${SWIPE_ROW_TRANSITION_MS}ms`;
  }, [offsetX]);

  useEffect(() => {
    const node = containerRef.current?.parentElement;
    if (!node) return;
    const handler = (e: MouseEvent) => {
      if (Date.now() - swipeEndedAtRef.current < POST_SWIPE_CLICK_SUPPRESS_MS) {
        swipeEndedAtRef.current = 0;
        e.stopPropagation();
        e.preventDefault();
      }
    };
    node.addEventListener("click", handler, true);
    return () => node.removeEventListener("click", handler, true);
  }, []);

  // Native TouchEvent listeners with passive:false — same pattern as
  // MobileRowActions. preventDefault() once horizontal locks the gesture
  // so iOS Safari doesn't take it over for native scroll mid-swipe.
  useEffect(() => {
    const overlay = containerRef.current;
    if (!overlay) return;

    let startX = 0;
    let startY = 0;
    let direction: "horizontal" | "vertical" | null = null;
    let active = false;

    const onTouchStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return;
      const t = e.touches[0]!;
      startX = t.clientX;
      startY = t.clientY;
      direction = null;
      active = true;
      gestureActiveRef.current = true;
    };

    const onTouchMove = (e: TouchEvent) => {
      if (!active || e.touches.length !== 1) return;
      const t = e.touches[0]!;
      const dx = t.clientX - startX;
      const dy = t.clientY - startY;
      const adx = Math.abs(dx);
      const ady = Math.abs(dy);

      if (direction === null) {
        if (adx < DIRECTION_DECIDE_PX && ady < DIRECTION_DECIDE_PX) return;
        direction = adx > ady ? "horizontal" : "vertical";
        if (direction === "vertical") {
          active = false;
          gestureActiveRef.current = false;
          return;
        }
      }
      if (direction !== "horizontal") return;

      e.preventDefault();
      // Channel rows only archive (right swipe). Negatives clamped to 0
      // so the row can't flop left into nothing.
      const live = Math.max(0, dx);
      offsetXRef.current = live;
      setOffsetX(live);
    };

    const onTouchEnd = () => {
      const wasHorizontal = direction === "horizontal";
      direction = null;
      active = false;
      gestureActiveRef.current = false;
      if (!wasHorizontal) return;

      const live = offsetXRef.current;
      const rowWidth = overlay.parentElement?.clientWidth ?? 360;
      if (live > SWIPE_INTENT_PX) {
        swipeEndedAtRef.current = Date.now();
      }
      if (live >= commitThresholdPx(rowWidth)) {
        archiveCommitTimeoutRef.current = setTimeout(() => {
          setOffsetX(0);
          onArchiveRef.current();
          archiveCommitTimeoutRef.current = null;
        }, ARCHIVE_COMMIT_DELAY_MS);
      } else {
        setOffsetX(0);
      }
    };

    overlay.addEventListener("touchstart", onTouchStart, { passive: false });
    overlay.addEventListener("touchmove", onTouchMove, { passive: false });
    overlay.addEventListener("touchend", onTouchEnd);
    overlay.addEventListener("touchcancel", onTouchEnd);
    return () => {
      overlay.removeEventListener("touchstart", onTouchStart);
      overlay.removeEventListener("touchmove", onTouchMove);
      overlay.removeEventListener("touchend", onTouchEnd);
      overlay.removeEventListener("touchcancel", onTouchEnd);
    };
  }, [setOffsetX]);

  return (
    <>
      <div
        ref={containerRef}
        className="absolute inset-0 sm:hidden"
        style={{ touchAction: "pan-y" }}
      />
      {offsetX > 0 && (
        <div
          className="absolute inset-y-0 left-0 flex items-center justify-start gap-2 bg-destructive px-4 text-white"
          style={{ width: Math.max(offsetX, 0) }}
          aria-hidden
        >
          <Archive className="size-4" />
          <span className="text-xs font-medium text-white">{strings.swipe_archive}</span>
        </div>
      )}
    </>
  );
  // No SimpleArchiveButton here — `CerebroSwipeArchive` already takes the
  // desktop fallback path (`!isMobile`) before reaching this component.
  // Rendering it again from the mobile path was redundant and added a
  // nested clickable that could confuse iOS Safari's tap-target heuristic.
}

function SimpleArchiveButton({ onArchive }: { onArchive: () => void }) {
  const strings = useCerebroInboxStrings();
  return (
    <span
      role="button"
      tabIndex={-1}
      title={strings.archive_label}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onArchive();
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.stopPropagation();
          onArchive();
        }
      }}
      className="absolute right-2 top-1/2 -translate-y-1/2 hidden h-7 w-7 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground sm:group-hover:inline-flex"
    >
      <Archive className="h-3.5 w-3.5" />
    </span>
  );
}

/**
 * Spejlvendt variant af {@link CerebroSwipeArchive} til archived-view (JEH-1166).
 *
 * Renders:
 * - Desktop: hover-only `ArchiveRestore` icon, click → onUnarchive.
 * - Mobile: full-row touch overlay; swipe-right past the same commit
 *   threshold as archive fires onUnarchive on release. The reveal background
 *   uses the brand color (not destructive) so the gesture reads as
 *   "restore", not "delete again".
 *
 * Kept as a sibling component to CerebroSwipeArchive rather than a flag on
 * the existing one — archived rows don't carry mute/mark-unread actions, so
 * the long-press drawer + swipe-left panel from CerebroInboxRowActions are
 * irrelevant here. A dedicated component keeps the surface honest.
 */
export function CerebroUnarchiveAction({
  onUnarchive,
}: {
  onUnarchive: () => void;
}) {
  const enabled = useFeatureFlag("cerebro_inbox_row_actions");
  const isMobile = useIsMobile();
  const strings = useCerebroInboxStrings();

  if (!enabled || !isMobile) {
    return <SimpleUnarchiveButton onUnarchive={onUnarchive} />;
  }

  return <SwipeUnarchiveOnly onUnarchive={onUnarchive} strings={strings} />;
}

function SimpleUnarchiveButton({ onUnarchive }: { onUnarchive: () => void }) {
  const strings = useCerebroInboxStrings();
  return (
    <span
      role="button"
      tabIndex={-1}
      title={strings.unarchive_label}
      aria-label={strings.unarchive_label}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onUnarchive();
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.stopPropagation();
          onUnarchive();
        }
      }}
      className="absolute right-2 top-1/2 -translate-y-1/2 hidden h-7 w-7 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground sm:group-hover:inline-flex"
    >
      <ArchiveRestore className="h-3.5 w-3.5" />
    </span>
  );
}

function SwipeUnarchiveOnly({
  onUnarchive,
  strings,
}: {
  onUnarchive: () => void;
  strings: ReturnType<typeof useCerebroInboxStrings>;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const offsetXRef = useRef(0);
  const [offsetX, setOffsetXState] = useState(0);
  const setOffsetX = useCallback((v: number) => {
    offsetXRef.current = v;
    setOffsetXState(v);
  }, []);
  const gestureActiveRef = useRef(false);
  const swipeEndedAtRef = useRef(0);

  const onUnarchiveRef = useRef(onUnarchive);
  useEffect(() => {
    onUnarchiveRef.current = onUnarchive;
  });

  useEffect(() => {
    const row = containerRef.current?.parentElement;
    if (!row) return;
    if (offsetX === 0) {
      row.style.transform = "";
    } else {
      row.style.transform = `translateX(${offsetX}px)`;
    }
    row.style.transition = gestureActiveRef.current ? "" : "transform 200ms";
  }, [offsetX]);

  useEffect(() => {
    const node = containerRef.current?.parentElement;
    if (!node) return;
    const handler = (e: MouseEvent) => {
      if (Date.now() - swipeEndedAtRef.current < POST_SWIPE_CLICK_SUPPRESS_MS) {
        swipeEndedAtRef.current = 0;
        e.stopPropagation();
        e.preventDefault();
      }
    };
    node.addEventListener("click", handler, true);
    return () => node.removeEventListener("click", handler, true);
  }, []);

  useEffect(() => {
    const overlay = containerRef.current;
    if (!overlay) return;

    let startX = 0;
    let startY = 0;
    let direction: "horizontal" | "vertical" | null = null;
    let active = false;

    const onTouchStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return;
      const t = e.touches[0]!;
      startX = t.clientX;
      startY = t.clientY;
      direction = null;
      active = true;
      gestureActiveRef.current = true;
    };

    const onTouchMove = (e: TouchEvent) => {
      if (!active || e.touches.length !== 1) return;
      const t = e.touches[0]!;
      const dx = t.clientX - startX;
      const dy = t.clientY - startY;
      const adx = Math.abs(dx);
      const ady = Math.abs(dy);

      if (direction === null) {
        if (adx < DIRECTION_DECIDE_PX && ady < DIRECTION_DECIDE_PX) return;
        direction = adx > ady ? "horizontal" : "vertical";
        if (direction === "vertical") {
          active = false;
          gestureActiveRef.current = false;
          return;
        }
      }
      if (direction !== "horizontal") return;

      e.preventDefault();
      // Mirror of archive: swipe-right reveals the unarchive cue.
      const live = Math.max(0, dx);
      offsetXRef.current = live;
      setOffsetX(live);
    };

    const onTouchEnd = () => {
      const wasHorizontal = direction === "horizontal";
      direction = null;
      active = false;
      gestureActiveRef.current = false;
      if (!wasHorizontal) return;

      const live = offsetXRef.current;
      const rowWidth = overlay.parentElement?.clientWidth ?? 360;
      if (live > SWIPE_INTENT_PX) {
        swipeEndedAtRef.current = Date.now();
      }
      if (live >= commitThresholdPx(rowWidth)) {
        setOffsetX(0);
        onUnarchiveRef.current();
      } else {
        setOffsetX(0);
      }
    };

    overlay.addEventListener("touchstart", onTouchStart, { passive: false });
    overlay.addEventListener("touchmove", onTouchMove, { passive: false });
    overlay.addEventListener("touchend", onTouchEnd);
    overlay.addEventListener("touchcancel", onTouchEnd);
    return () => {
      overlay.removeEventListener("touchstart", onTouchStart);
      overlay.removeEventListener("touchmove", onTouchMove);
      overlay.removeEventListener("touchend", onTouchEnd);
      overlay.removeEventListener("touchcancel", onTouchEnd);
    };
  }, [setOffsetX]);

  return (
    <>
      <div
        ref={containerRef}
        className="absolute inset-0 sm:hidden"
        style={{ touchAction: "pan-y" }}
      />
      {offsetX > 0 && (
        <div
          className="absolute inset-y-0 left-0 flex items-center justify-start gap-2 bg-brand px-4 text-brand-foreground"
          style={{ width: Math.max(offsetX, 0) }}
          aria-hidden
        >
          <ArchiveRestore className="size-4" />
          <span className="text-xs font-medium">{strings.swipe_unarchive}</span>
        </div>
      )}
    </>
  );
}
