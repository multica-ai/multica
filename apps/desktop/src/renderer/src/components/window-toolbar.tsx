import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEventHandler,
} from "react";
import { ChevronLeft, ChevronRight, History } from "lucide-react";
import { SidebarTrigger } from "@multica/ui/components/ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  ResourceLeadingVisual,
  useTabPresentation,
} from "@multica/views/layout";
import { useNavigation } from "@multica/views/navigation";
import { useTabHistory } from "@/hooks/use-tab-history";
import { resourceKeyForUrl } from "@/stores/tab-store";

export const WINDOW_TOOLBAR_CLEARANCE = 208;
const LONG_PRESS_DURATION_MS = 500;
const LONG_PRESS_MOVE_TOLERANCE_PX = 8;
const MAX_HISTORY_MENU_ITEMS = 30;

type HistoryMenuMode = "all" | "back" | "forward";

interface OpenHistoryMenu {
  mode: HistoryMenuMode;
  anchor: HTMLElement;
}

interface LongPressHandlers {
  onPointerDown: PointerEventHandler<HTMLButtonElement>;
  onPointerMove: PointerEventHandler<HTMLButtonElement>;
  onPointerUp: PointerEventHandler<HTMLButtonElement>;
  onPointerCancel: PointerEventHandler<HTMLButtonElement>;
  onPointerLeave: PointerEventHandler<HTMLButtonElement>;
  consumeClick: () => boolean;
}

function useLongPress(
  disabled: boolean,
  onLongPress: (anchor: HTMLButtonElement) => void,
): LongPressHandlers {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const originRef = useRef<{ x: number; y: number } | null>(null);
  const suppressClickRef = useRef(false);

  const clearPressTimer = useCallback(() => {
    if (timerRef.current !== null) clearTimeout(timerRef.current);
    timerRef.current = null;
    originRef.current = null;
  }, []);

  useEffect(() => {
    return clearPressTimer;
  }, [clearPressTimer]);

  const onPointerDown = useCallback<PointerEventHandler<HTMLButtonElement>>(
    (event) => {
      if (disabled || event.button !== 0) return;
      clearPressTimer();
      // A completed long press suppresses the click produced by that same
      // pointer gesture. Starting a new gesture makes any stale suppression
      // irrelevant (for example, if the previous pointer left the button and
      // therefore produced no click).
      suppressClickRef.current = false;
      originRef.current = { x: event.clientX, y: event.clientY };
      const anchor = event.currentTarget;
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        originRef.current = null;
        suppressClickRef.current = true;
        onLongPress(anchor);
      }, LONG_PRESS_DURATION_MS);
    },
    [clearPressTimer, disabled, onLongPress],
  );

  const onPointerMove = useCallback<PointerEventHandler<HTMLButtonElement>>(
    (event) => {
      const origin = originRef.current;
      if (!origin) return;
      if (
        Math.abs(event.clientX - origin.x) > LONG_PRESS_MOVE_TOLERANCE_PX ||
        Math.abs(event.clientY - origin.y) > LONG_PRESS_MOVE_TOLERANCE_PX
      ) {
        clearPressTimer();
      }
    },
    [clearPressTimer],
  );

  const consumeClick = useCallback(() => {
    if (!suppressClickRef.current) return false;
    suppressClickRef.current = false;
    return true;
  }, []);

  return {
    onPointerDown,
    onPointerMove,
    onPointerUp: clearPressTimer,
    onPointerCancel: clearPressTimer,
    onPointerLeave: clearPressTimer,
    consumeClick,
  };
}

export function historyIndicesForMenu(
  mode: Exclude<HistoryMenuMode, "all">,
  currentIndex: number,
  historyLength: number,
): number[] {
  if (mode === "back") {
    return Array.from(
      { length: Math.min(currentIndex, MAX_HISTORY_MENU_ITEMS) },
      (_, offset) => currentIndex - offset - 1,
    );
  }
  return Array.from(
    {
      length: Math.min(
        Math.max(0, historyLength - currentIndex - 1),
        MAX_HISTORY_MENU_ITEMS,
      ),
    },
    (_, offset) => currentIndex + offset + 1,
  );
}

export function browsingHistoryForMenu(
  browsingHistory: string[],
  currentUrl: string | undefined,
): string[] {
  const currentResource = currentUrl
    ? resourceKeyForUrl(currentUrl)
    : undefined;
  return browsingHistory
    .filter((url) => resourceKeyForUrl(url) !== currentResource)
    .slice(0, MAX_HISTORY_MENU_ITEMS);
}

function HistoryMenuItem({
  url,
  onSelect,
}: {
  url: string;
  onSelect: () => void;
}) {
  const { visual, title } = useTabPresentation(url);

  return (
    <DropdownMenuItem
      className="h-8 min-w-0 gap-2 px-2"
      onClick={onSelect}
    >
      <ResourceLeadingVisual visual={visual} />
      <span className="min-w-0 flex-1 truncate">{title}</span>
    </DropdownMenuItem>
  );
}

export function WindowToolbar() {
  const {
    canGoBack,
    canGoForward,
    historyEntries,
    historyIndex,
    browsingHistory,
    goBack,
    goForward,
    goToHistoryIndex,
  } = useTabHistory();
  const { push } = useNavigation();
  const [menu, setMenu] = useState<OpenHistoryMenu | null>(null);

  const openMenu = useCallback((mode: HistoryMenuMode, anchor: HTMLElement) => {
    setMenu({ mode, anchor });
  }, []);
  const openBackMenu = useCallback(
    (anchor: HTMLButtonElement) => openMenu("back", anchor),
    [openMenu],
  );
  const openForwardMenu = useCallback(
    (anchor: HTMLButtonElement) => openMenu("forward", anchor),
    [openMenu],
  );
  const backPress = useLongPress(!canGoBack, openBackMenu);
  const forwardPress = useLongPress(!canGoForward, openForwardMenu);

  const menuIndices = useMemo(
    () =>
      menu && menu.mode !== "all"
        ? historyIndicesForMenu(menu.mode, historyIndex, historyEntries.length)
        : [],
    [historyEntries.length, historyIndex, menu],
  );
  const browsingMenuEntries = useMemo(
    () =>
      browsingHistoryForMenu(
        browsingHistory,
        historyEntries[historyIndex],
      ),
    [browsingHistory, historyEntries, historyIndex],
  );
  const menuLabel =
    menu?.mode === "back"
      ? "Back history"
      : menu?.mode === "forward"
        ? "Forward history"
        : "Recently viewed";
  const navButtonClassName =
    "flex size-7 items-center justify-center rounded-md text-faint-foreground transition-colors duration-150 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 disabled:pointer-events-none disabled:opacity-30 motion-reduce:transition-none";

  const selectHistoryIndex = useCallback(
    (index: number) => {
      goToHistoryIndex(index);
      setMenu(null);
    },
    [goToHistoryIndex],
  );
  const selectBrowsingHistory = useCallback(
    (url: string) => {
      push(url);
      setMenu(null);
    },
    [push],
  );

  return (
    <div
      className="fixed left-0 top-0 z-30 flex h-12 w-[208px] shrink-0 items-center px-3"
      style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
    >
      <div
        className="flex items-center gap-1 pl-16"
        style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      >
        <SidebarTrigger
          className="size-7 text-faint-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
        />
        <button
          type="button"
          disabled={browsingMenuEntries.length === 0}
          aria-label="History"
          aria-haspopup="menu"
          aria-expanded={menu?.mode === "all"}
          title="History"
          className={navButtonClassName}
          onClick={(event) => openMenu("all", event.currentTarget)}
        >
          <History aria-hidden className="size-4" />
        </button>
        <button
          type="button"
          disabled={!canGoBack}
          aria-label="Go back"
          aria-haspopup="menu"
          aria-expanded={menu?.mode === "back"}
          title="Go back (press and hold for history)"
          className={navButtonClassName}
          onPointerDown={backPress.onPointerDown}
          onPointerMove={backPress.onPointerMove}
          onPointerUp={backPress.onPointerUp}
          onPointerCancel={backPress.onPointerCancel}
          onPointerLeave={backPress.onPointerLeave}
          onContextMenu={(event) => {
            event.preventDefault();
            openMenu("back", event.currentTarget);
          }}
          onKeyDown={(event) => {
            if (event.key !== "ArrowDown") return;
            event.preventDefault();
            openMenu("back", event.currentTarget);
          }}
          onClick={(event) => {
            if (backPress.consumeClick()) {
              event.preventDefault();
              return;
            }
            goBack();
          }}
        >
          <ChevronLeft aria-hidden className="size-4" />
        </button>
        <button
          type="button"
          disabled={!canGoForward}
          aria-label="Go forward"
          aria-haspopup="menu"
          aria-expanded={menu?.mode === "forward"}
          title="Go forward (press and hold for history)"
          className={navButtonClassName}
          onPointerDown={forwardPress.onPointerDown}
          onPointerMove={forwardPress.onPointerMove}
          onPointerUp={forwardPress.onPointerUp}
          onPointerCancel={forwardPress.onPointerCancel}
          onPointerLeave={forwardPress.onPointerLeave}
          onContextMenu={(event) => {
            event.preventDefault();
            openMenu("forward", event.currentTarget);
          }}
          onKeyDown={(event) => {
            if (event.key !== "ArrowDown") return;
            event.preventDefault();
            openMenu("forward", event.currentTarget);
          }}
          onClick={(event) => {
            if (forwardPress.consumeClick()) {
              event.preventDefault();
              return;
            }
            goForward();
          }}
        >
          <ChevronRight aria-hidden className="size-4" />
        </button>
      </div>

      <DropdownMenu
        open={menu !== null}
        onOpenChange={(open) => {
          if (!open) setMenu(null);
        }}
      >
        <DropdownMenuContent
          align="start"
          anchor={menu?.anchor}
          className="w-80 max-w-[calc(100vw-1rem)] motion-reduce:animate-none motion-reduce:transition-none"
        >
          <DropdownMenuGroup>
            <DropdownMenuLabel>{menuLabel}</DropdownMenuLabel>
            {menu?.mode === "all"
              ? browsingMenuEntries.map((url) => (
                  <HistoryMenuItem
                    key={url}
                    url={url}
                    onSelect={() => selectBrowsingHistory(url)}
                  />
                ))
              : menuIndices.map((index) => (
                  <HistoryMenuItem
                    key={`${index}:${historyEntries[index]}`}
                    url={historyEntries[index]}
                    onSelect={() => selectHistoryIndex(index)}
                  />
                ))}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
