"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type PointerEvent,
  type ReactNode,
} from "react";
import {
  Archive,
  BellOff,
  BellRing,
  MailOpen,
  MailWarning,
  MoreHorizontal,
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
  DIRECTION_DECIDE_PX,
  HORIZONTAL_DEADZONE_PX,
  LEFT_PANEL_REVEAL_PX,
  LONG_PRESS_MS,
  VERTICAL_BAIL_PX,
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
        renderDrawerMenu={() => menuItems}
      />
    );
  }
  return (
    <DesktopRowActions
      strings={strings}
      menuItems={menuItems}
      onArchive={onArchive}
    />
  );
}

/* -------------------------------------------------------------------------- */
/*  Desktop                                                                    */
/* -------------------------------------------------------------------------- */

interface DesktopProps {
  menuItems: ReactNode;
  onArchive: () => void;
  strings: ReturnType<typeof useCerebroInboxStrings>;
}

function DesktopRowActions({ menuItems, onArchive, strings }: DesktopProps) {
  // Two menus share `menuItems`:
  //   1. Hover: the inline "..." DropdownMenuTrigger anchors against the icon.
  //   2. Right-click: a separate DropdownMenu whose trigger is a 1×1 phantom
  //      anchor positioned at the click coords (fixed, document-relative), so
  //      the popover lands at the click point — Linear/Gmail style.
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [hoverMenuOpen, setHoverMenuOpen] = useState(false);
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
          when the parent row is hovered. */}
      <div
        className="absolute right-2 top-1/2 -translate-y-1/2 hidden items-center gap-1 sm:group-hover:flex"
        onClick={(e) => e.stopPropagation()}
      >
        <IconButton
          aria-label={strings.archive_label}
          onClick={onArchive}
          title={strings.archive_tooltip}
        >
          <Archive className="size-3.5" />
        </IconButton>

        <DropdownMenu open={hoverMenuOpen} onOpenChange={setHoverMenuOpen}>
          {/* role=button span (not <button>) so the trigger nests cleanly
              inside the parent InboxListItemShell <button>, which otherwise
              produces invalid HTML and unstable click delivery on mobile. */}
          <DropdownMenuTrigger
            render={(props) => (
              <span
                role="button"
                tabIndex={-1}
                aria-label={strings.more_actions}
                title={strings.more_actions}
                {...props}
                className={cn(
                  "flex h-7 w-7 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground",
                  props.className,
                )}
              >
                <MoreHorizontal className="size-3.5" />
              </span>
            )}
          />
          <DropdownMenuContent align="end" className="min-w-44">
            {menuItems}
          </DropdownMenuContent>
        </DropdownMenu>
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
  renderDrawerMenu: () => ReactNode;
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
  const startX = useRef<number | null>(null);
  const startY = useRef<number | null>(null);
  // null until the gesture has moved past DIRECTION_DECIDE_PX in either
  // axis — at which point we lock the dominant direction so subsequent
  // movement can't flip mid-gesture.
  const lockedDirRef = useRef<"horizontal" | "vertical" | null>(null);
  const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // offsetX is mirrored in a ref so the pointerup handler reads the live
  // gesture distance (closure capture from setState lags by one render).
  const offsetXRef = useRef(0);
  const [offsetX, setOffsetXState] = useState(0);
  const setOffsetX = useCallback((v: number) => {
    offsetXRef.current = v;
    setOffsetXState(v);
  }, []);
  const [revealed, setRevealed] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  // Set true at the moment a swipe-right archive (or swipe-left reveal)
  // commits, so the click event the browser synthesises after the touch
  // sequence is swallowed by the parent shell <button> (otherwise the row's
  // onClick fires and we navigate to the issue right after archiving it).
  const swipeJustCompletedRef = useRef(false);

  // Apply the offset transform to the parent row so the user sees the row
  // visually follow the finger.
  useEffect(() => {
    const row = containerRef.current?.parentElement;
    if (!row) return;
    if (offsetX === 0 && !revealed) {
      row.style.transform = "";
    } else {
      const x = revealed && offsetX === 0 ? -LEFT_PANEL_REVEAL_PX : offsetX;
      row.style.transform = `translateX(${x}px)`;
    }
    row.style.transition = startX.current === null ? "transform 200ms" : "";
  }, [offsetX, revealed]);

  // Capture-phase click listener on the parent shell <button>: when a swipe
  // just committed, the synthetic click fired by the browser after touchend
  // is intercepted here and prevented from triggering the row's onClick
  // (navigation). Without this, swipe-right "archives + navigates", which
  // looks to the user like the gesture didn't work.
  useEffect(() => {
    const node = containerRef.current?.parentElement;
    if (!node) return;
    const handler = (e: MouseEvent) => {
      if (swipeJustCompletedRef.current) {
        swipeJustCompletedRef.current = false;
        e.stopPropagation();
        e.preventDefault();
      }
    };
    node.addEventListener("click", handler, true);
    return () => node.removeEventListener("click", handler, true);
  }, []);

  const cancelLongPress = useCallback(() => {
    if (longPressTimer.current) {
      clearTimeout(longPressTimer.current);
      longPressTimer.current = null;
    }
  }, []);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.pointerType !== "touch") return;
    startX.current = e.clientX;
    startY.current = e.clientY;
    lockedDirRef.current = null;
    longPressTimer.current = setTimeout(() => {
      setDrawerOpen(true);
      startX.current = null; // disable swipe once long-press fires
    }, LONG_PRESS_MS);
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (startX.current === null) return;
    const dx = e.clientX - startX.current;
    const dy = e.clientY - (startY.current ?? 0);
    const adx = Math.abs(dx);
    const ady = Math.abs(dy);

    // Direction lock — pick once movement is past the decision zone.
    if (lockedDirRef.current === null) {
      if (adx < DIRECTION_DECIDE_PX && ady < DIRECTION_DECIDE_PX) return;
      if (ady > adx && ady > VERTICAL_BAIL_PX) {
        // Vertical scroll wins; bail and let the browser take over.
        cancelLongPress();
        startX.current = null;
        lockedDirRef.current = "vertical";
        setOffsetX(0);
        return;
      }
      lockedDirRef.current = "horizontal";
    }

    if (lockedDirRef.current !== "horizontal") return;
    cancelLongPress();

    // Apply a deadzone so the first ~12px doesn't visibly move the row —
    // small touch jitter shouldn't drag it. Past the deadzone, subtract it
    // so the row tracks the finger naturally from there.
    const adjusted =
      dx > HORIZONTAL_DEADZONE_PX
        ? dx - HORIZONTAL_DEADZONE_PX
        : dx < -HORIZONTAL_DEADZONE_PX
          ? dx + HORIZONTAL_DEADZONE_PX
          : 0;
    // Clamp: right-swipe up to row width, left-swipe up to panel width.
    const clamped = Math.max(
      -LEFT_PANEL_REVEAL_PX - 16,
      Math.min(adjusted, 240),
    );
    setOffsetX(clamped);
  };

  const onPointerUp = () => {
    cancelLongPress();
    if (startX.current === null) {
      // Long-press fired or pointer was cancelled.
      lockedDirRef.current = null;
      return;
    }
    startX.current = null;
    lockedDirRef.current = null;
    const live = offsetXRef.current;
    const rowWidth = containerRef.current?.parentElement?.clientWidth ?? 360;
    const commitPx = commitThresholdPx(rowWidth);
    if (live >= commitPx) {
      swipeJustCompletedRef.current = true;
      setOffsetX(0);
      setRevealed(false);
      onArchive();
      return;
    }
    if (live <= -commitPx) {
      swipeJustCompletedRef.current = true;
      setOffsetX(0);
      setRevealed(true);
      return;
    }
    setOffsetX(0);
  };

  const closePanel = useCallback(() => {
    setRevealed(false);
    setOffsetX(0);
  }, []);

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
      className="absolute inset-y-0 left-0 flex items-center justify-start gap-2 bg-destructive px-4 text-destructive-foreground"
      style={{ width: Math.max(offsetX, 0) }}
      aria-hidden
    >
      <Archive className="size-4" />
      <span className="text-xs font-medium">{strings.swipe_archive}</span>
    </div>
  );

  // Swipe-left action panel.
  const leftPanel = (offsetX < 0 || revealed) && (
    <div
      className="absolute inset-y-0 right-0 flex items-stretch"
      style={{ width: revealed ? LEFT_PANEL_REVEAL_PX : Math.max(-offsetX, 0) }}
      aria-hidden={!revealed}
    >
      <span
        role="button"
        tabIndex={-1}
        onClick={(e) => {
          e.stopPropagation();
          closePanel();
          onToggleRead();
        }}
        className="flex flex-1 flex-col items-center justify-center gap-1 bg-brand text-xs font-medium text-brand-foreground"
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
          e.stopPropagation();
          closePanel();
          onToggleMute();
        }}
        className="flex flex-1 flex-col items-center justify-center gap-1 bg-amber-500 text-xs font-medium text-white"
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
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        // Don't swallow taps — only swipes apply transforms. The wrapper button
        // still receives onClick for navigation.
        style={{ touchAction: "pan-y" }}
      />
      {archiveBg}
      {leftPanel}
      <Drawer open={drawerOpen} onOpenChange={setDrawerOpen}>
        <DrawerContent>
          <DrawerHeader>
            <DrawerTitle>{strings.drawer_title}</DrawerTitle>
          </DrawerHeader>
          <div className="px-4 pb-6">{renderDrawerMenu()}</div>
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

/* -------------------------------------------------------------------------- */
/*  Helpers                                                                    */
/* -------------------------------------------------------------------------- */

function IconButton({
  children,
  className,
  onClick,
  ...props
}: React.HTMLAttributes<HTMLSpanElement>) {
  // role=button span (not <button>) — the inbox row shell wraps the whole
  // row in a <button>, and a real <button> nested inside another <button>
  // is invalid HTML and produces unstable click delivery on mobile Safari.
  return (
    <span
      role="button"
      tabIndex={-1}
      onClick={onClick}
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
 * channels and DMs. The full row-actions surface is item-specific (mute /
 * mark-unread keyed off `inbox_item.id`), but archive applies uniformly to
 * any row in the inbox queue, so this slimmer component gives channel rows
 * the same swipe-right ergonomics without dragging in the rest of the menu.
 *
 * Renders:
 * - Desktop: hover-only archive icon (matches the upstream simple icon)
 * - Mobile: full-row touch overlay; swipe-right ≥80px archives on release
 *
 * Reuses the same click-suppression pattern as `MobileRowActions` so the
 * swipe gesture doesn't leak through to the parent row's onClick navigate.
 */
export function CerebroSwipeArchive({ onArchive }: { onArchive: () => void }) {
  const enabled = useFeatureFlag("cerebro_inbox_row_actions");
  const isMobile = useIsMobile();
  const strings = useCerebroInboxStrings();

  if (!enabled || !isMobile) {
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
  const startX = useRef<number | null>(null);
  const startY = useRef<number | null>(null);
  const lockedDirRef = useRef<"horizontal" | "vertical" | null>(null);
  const offsetXRef = useRef(0);
  const [offsetX, setOffsetXState] = useState(0);
  const setOffsetX = useCallback((v: number) => {
    offsetXRef.current = v;
    setOffsetXState(v);
  }, []);
  const swipeJustCompletedRef = useRef(false);

  useEffect(() => {
    const row = containerRef.current?.parentElement;
    if (!row) return;
    if (offsetX === 0) {
      row.style.transform = "";
    } else {
      row.style.transform = `translateX(${offsetX}px)`;
    }
    row.style.transition = startX.current === null ? "transform 200ms" : "";
  }, [offsetX]);

  useEffect(() => {
    const node = containerRef.current?.parentElement;
    if (!node) return;
    const handler = (e: MouseEvent) => {
      if (swipeJustCompletedRef.current) {
        swipeJustCompletedRef.current = false;
        e.stopPropagation();
        e.preventDefault();
      }
    };
    node.addEventListener("click", handler, true);
    return () => node.removeEventListener("click", handler, true);
  }, []);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.pointerType !== "touch") return;
    startX.current = e.clientX;
    startY.current = e.clientY;
    lockedDirRef.current = null;
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (startX.current === null) return;
    const dx = e.clientX - startX.current;
    const dy = e.clientY - (startY.current ?? 0);
    const adx = Math.abs(dx);
    const ady = Math.abs(dy);

    if (lockedDirRef.current === null) {
      if (adx < DIRECTION_DECIDE_PX && ady < DIRECTION_DECIDE_PX) return;
      if (ady > adx && ady > VERTICAL_BAIL_PX) {
        startX.current = null;
        lockedDirRef.current = "vertical";
        setOffsetX(0);
        return;
      }
      lockedDirRef.current = "horizontal";
    }
    if (lockedDirRef.current !== "horizontal") return;

    // Right-only with deadzone — channel rows can only archive, can't reveal
    // a left-side panel.
    const adjusted = dx > HORIZONTAL_DEADZONE_PX ? dx - HORIZONTAL_DEADZONE_PX : 0;
    setOffsetX(Math.max(0, Math.min(adjusted, 240)));
  };

  const onPointerUp = () => {
    if (startX.current === null) {
      lockedDirRef.current = null;
      return;
    }
    startX.current = null;
    lockedDirRef.current = null;
    const rowWidth = containerRef.current?.parentElement?.clientWidth ?? 360;
    if (offsetXRef.current >= commitThresholdPx(rowWidth)) {
      swipeJustCompletedRef.current = true;
      setOffsetX(0);
      onArchive();
      return;
    }
    setOffsetX(0);
  };

  return (
    <>
      <div
        ref={containerRef}
        className="absolute inset-0 sm:hidden"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        style={{ touchAction: "pan-y" }}
      />
      {offsetX > 0 && (
        <div
          className="absolute inset-y-0 left-0 flex items-center justify-start gap-2 bg-destructive px-4 text-destructive-foreground"
          style={{ width: Math.max(offsetX, 0) }}
          aria-hidden
        >
          <Archive className="size-4" />
          <span className="text-xs font-medium">{strings.swipe_archive}</span>
        </div>
      )}
      <SimpleArchiveButton onArchive={onArchive} />
    </>
  );
}

function SimpleArchiveButton({ onArchive }: { onArchive: () => void }) {
  const strings = useCerebroInboxStrings();
  return (
    <span
      role="button"
      tabIndex={-1}
      title={strings.archive_label}
      onClick={(e) => {
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
