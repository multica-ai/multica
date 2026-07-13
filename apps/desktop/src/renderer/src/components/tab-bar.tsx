import {
  Fragment,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import { motion, useReducedMotion } from "motion/react";
import {
  Inbox,
  CircleUser,
  ListTodo,
  Bot,
  Monitor,
  BookOpenText,
  Settings,
  X,
  Plus,
  Pin,
  PinOff,
  ListX,
  type LucideIcon,
} from "lucide-react";
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  closestCenter,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  horizontalListSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import {
  restrictToHorizontalAxis,
  restrictToParentElement,
} from "@dnd-kit/modifiers";
import { CSS } from "@dnd-kit/utilities";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@multica/ui/components/ui/context-menu";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import {
  useTabStore,
  useActiveGroup,
  resolveRouteIcon,
  type Tab,
} from "@/stores/tab-store";
import { paths } from "@multica/core/paths";

const TAB_ICONS: Record<string, LucideIcon> = {
  Inbox,
  CircleUser,
  ListTodo,
  Bot,
  Monitor,
  BookOpenText,
  Settings,
};

const TAB_SCROLL_FADE_SIZE = 24;
const TAB_ENTRY_EASE = [0.22, 1, 0.36, 1] as const;

// Chrome-style merged tab: the active tab shares the content surface's fill
// and flares into it through concave bottom corners. Each flare is a small
// square whose radial gradient carves a quarter-circle notch (shell shows
// through), strokes a 1px arc that continues the tab's side border into the
// content card's top ring, and fills the rest with the surface color. The
// 0.4px stop spread anti-aliases the arc.
const TAB_FLARE_RADIUS = 10;
const tabFlareBackground = (side: "left" | "right") => {
  const r = TAB_FLARE_RADIUS;
  return `radial-gradient(circle at top ${side}, transparent ${r - 1.2}px, var(--surface-border) ${r - 0.8}px, var(--surface-border) ${r - 0.2}px, var(--page-canvas) ${r + 0.2}px)`;
};

type TabSnapshot = {
  workspaceSlug: string | null;
  ids: Set<string>;
};

function getAddedTabIds(
  previous: TabSnapshot | null,
  workspaceSlug: string | null,
  currentIds: string[],
) {
  if (
    !previous ||
    previous.workspaceSlug !== workspaceSlug ||
    currentIds.length <= previous.ids.size
  ) {
    return [];
  }

  return currentIds.filter((id) => !previous.ids.has(id));
}

function getTabElement(
  scroller: HTMLDivElement,
  tabId?: string,
): HTMLElement | null {
  if (!tabId) {
    return scroller.querySelector<HTMLElement>('[data-tab-active="true"]');
  }

  return Array.from(
    scroller.querySelectorAll<HTMLElement>("[data-tab-id]"),
  ).find((candidate) => candidate.dataset.tabId === tabId) ?? null;
}

function getTabScrollTarget(
  scroller: HTMLDivElement,
  tab: HTMLElement,
): number {
  const scrollerRect = scroller.getBoundingClientRect();
  const tabRect = tab.getBoundingClientRect();
  const maxScrollLeft = Math.max(0, scroller.scrollWidth - scroller.clientWidth);
  const hasHiddenLeft = scroller.scrollLeft > 1;
  const hasHiddenRight = scroller.scrollLeft < maxScrollLeft - 1;
  const visibleLeft =
    scrollerRect.left + (hasHiddenLeft ? TAB_SCROLL_FADE_SIZE : 0);
  const visibleRight =
    scrollerRect.right - (hasHiddenRight ? TAB_SCROLL_FADE_SIZE : 0);

  if (tabRect.left < visibleLeft) {
    return Math.max(0, scroller.scrollLeft - (visibleLeft - tabRect.left));
  }
  if (tabRect.right > visibleRight) {
    return Math.min(
      maxScrollLeft,
      scroller.scrollLeft + (tabRect.right - visibleRight),
    );
  }
  return scroller.scrollLeft;
}

// Keep scrolling scoped to the strip. Native scrollIntoView can also move
// scrollable desktop-shell ancestors and displace the whole window chrome.
function keepTabVisible(
  scroller: HTMLDivElement | null,
  tabId?: string,
  behavior: ScrollBehavior = "auto",
) {
  if (!scroller) return;
  const tab = getTabElement(scroller, tabId);
  if (!tab) return;

  const target = getTabScrollTarget(scroller, tab);
  if (Math.abs(target - scroller.scrollLeft) <= 1) return;

  if (behavior === "smooth" && typeof scroller.scrollTo === "function") {
    scroller.scrollTo({ left: target, behavior: "smooth" });
    return;
  }
  scroller.scrollLeft = target;
}

function SortableTabItem({
  tab,
  isActive,
  isOnly,
  canCloseOthers,
  isNew,
  shouldReduceMotion,
  showSeparator,
}: {
  tab: Tab;
  isActive: boolean;
  /**
   * True iff this is the only tab in the workspace. Hiding X on the last
   * tab matches existing behavior and avoids the surprise of the store's
   * last-tab reseed kicking in. Pinned tabs always hide X (RFC §3 D3c).
   */
  isOnly: boolean;
  canCloseOthers: boolean;
  isNew: boolean;
  shouldReduceMotion: boolean;
  /** Hairline on the tab's left edge — hidden next to the active tab. */
  showSeparator: boolean;
}) {
  const setActiveTab = useTabStore((s) => s.setActiveTab);
  const closeTab = useTabStore((s) => s.closeTab);
  const closeOtherTabs = useTabStore((s) => s.closeOtherTabs);
  const togglePin = useTabStore((s) => s.togglePin);

  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: tab.id });

  // Pinned tabs swap the route icon for a Pin glyph as the static "I am
  // pinned" indicator (RFC §3 D1v-iv FINAL). The route information is still
  // present in the title, and this avoids a hard left accent border that read
  // as visually heavy in light mode.
  const LeadingIcon = tab.pinned ? Pin : TAB_ICONS[tab.icon];

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    WebkitAppRegion: "no-drag",
    zIndex: isDragging ? 20 : undefined,
  } as React.CSSProperties;

  const handleClick = () => {
    if (isActive) return;
    setActiveTab(tab.id);
  };

  const handleClose = (e: React.MouseEvent) => {
    e.stopPropagation();
    closeTab(tab.id);
  };

  const handleTogglePin = (e: React.MouseEvent) => {
    e.stopPropagation();
    togglePin(tab.id);
  };

  const stopDragOnAction = (e: React.PointerEvent) => {
    e.stopPropagation();
  };

  // Pinned tabs keep their full title (RFC §3 D1v-ii FINAL). The only visual
  // differences vs. unpinned tabs are the leading Pin icon (swapped in above)
  // and the suppressed X (closing requires explicit Unpin). Pin/Unpin is
  // reachable via the hover action button below and the right-click menu.
  const showCloseButton = !tab.pinned && !isOnly;
  const [isEntering, setIsEntering] = useState(isNew && !shouldReduceMotion);
  const [showAddedHighlight, setShowAddedHighlight] = useState(isNew);

  useEffect(() => {
    if (!isDragging) return;
    setIsEntering(false);
    setShowAddedHighlight(false);
  }, [isDragging]);

  const tabButton = (
    <button
      type="button"
      {...attributes}
      {...listeners}
      onClick={handleClick}
      aria-label={tab.pinned ? `${tab.title} (pinned)` : tab.title}
      data-tab-active={isActive ? "true" : undefined}
      data-tab-entering={isEntering ? "true" : undefined}
      title={tab.pinned ? `${tab.title} (pinned)` : undefined}
      style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      className={cn(
        "group relative flex size-full min-w-0 items-center gap-1.5 px-2.5 text-xs transition-colors",
        "select-none cursor-default",
        isActive
          ? "font-medium text-foreground"
          : "text-muted-foreground hover:text-sidebar-accent-foreground",
        isDragging && "opacity-60",
      )}
    >
      {LeadingIcon && <LeadingIcon className="size-3.5 shrink-0" />}
      <span
        className="min-w-0 flex-1 overflow-hidden whitespace-nowrap text-left"
        style={{
          maskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
          WebkitMaskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
        }}
      >
        {tab.title}
      </span>
      <span
        onClick={handleTogglePin}
        onPointerDown={stopDragOnAction}
        role="button"
        aria-label={tab.pinned ? "Unpin tab" : "Pin tab"}
        title={tab.pinned ? "Unpin tab" : "Pin tab"}
        className="hidden size-3.5 shrink-0 items-center justify-center rounded-sm text-muted-foreground transition-colors group-hover:flex hover:bg-muted-foreground/20 hover:text-foreground"
      >
        {tab.pinned ? <PinOff className="size-2.5" /> : <Pin className="size-2.5" />}
      </span>
      {showCloseButton && (
        <span
          onClick={handleClose}
          onPointerDown={stopDragOnAction}
          role="button"
          aria-label="Close tab"
          className="hidden size-3.5 shrink-0 items-center justify-center rounded-sm text-muted-foreground transition-colors group-hover:flex hover:bg-muted-foreground/20 hover:text-foreground"
        >
          <X className="size-2.5" />
        </span>
      )}
    </button>
  );

  return (
    <div
      ref={setNodeRef}
      style={style}
      data-tab-frame
      data-tab-id={tab.id}
      className={cn("h-9 w-40 min-w-32", isActive && "z-10")}
    >
      <motion.div
        className="group/tab relative size-full"
        initial={isEntering ? { opacity: 0, x: 8 } : false}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: isEntering ? 0.18 : 0, ease: TAB_ENTRY_EASE }}
        onAnimationComplete={() => setIsEntering(false)}
      >
        {isActive ? (
          // Merged-tab chrome: a bordered cap, a borderless base whose fill
          // runs into the content card below (covering its top ring), and two
          // flares whose arcs hand the keyline over to the card's ring. The
          // flares overlap the tab edge by 1px so arc and side border meet.
          <span
            aria-hidden
            className={cn(
              "pointer-events-none absolute inset-0",
              isDragging && "opacity-60",
            )}
          >
            <span className="absolute inset-x-0 top-0 bottom-2.5 rounded-t-lg border border-b-0 border-surface-border bg-page-canvas" />
            <span className="absolute inset-x-0 bottom-0 h-2.5 bg-page-canvas" />
            <span
              className="absolute bottom-0 size-2.5"
              style={{ left: -TAB_FLARE_RADIUS + 1, background: tabFlareBackground("left") }}
            />
            <span
              className="absolute bottom-0 size-2.5"
              style={{ right: -TAB_FLARE_RADIUS + 1, background: tabFlareBackground("right") }}
            />
          </span>
        ) : (
          <span
            aria-hidden
            className="pointer-events-none absolute inset-x-0.5 top-1 bottom-1 rounded-lg bg-sidebar-accent opacity-0 transition-opacity group-hover/tab:opacity-100"
          />
        )}
        {showSeparator && (
          <span
            aria-hidden
            className="pointer-events-none absolute left-0 top-1/2 h-4 w-px -translate-y-1/2 bg-border"
          />
        )}
        <ContextMenu>
          <ContextMenuTrigger render={tabButton} />
          <ContextMenuContent>
            <ContextMenuItem onClick={() => togglePin(tab.id)}>
              {tab.pinned ? (
                <>
                  <PinOff />
                  Unpin tab
                </>
              ) : (
                <>
                  <Pin />
                  Pin tab
                </>
              )}
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem
              variant="destructive"
              disabled={tab.pinned || isOnly}
              onClick={() => closeTab(tab.id)}
            >
              <X />
              Close tab
            </ContextMenuItem>
            <ContextMenuItem
              variant="destructive"
              disabled={!canCloseOthers}
              onClick={() => closeOtherTabs(tab.id)}
            >
              <ListX />
              Close other tabs
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
        {showAddedHighlight && (
          <motion.span
            aria-hidden
            className="pointer-events-none absolute inset-x-0.5 top-1 bottom-1 rounded-lg bg-primary/10 ring-1 ring-inset ring-primary/20"
            initial={{ opacity: shouldReduceMotion ? 0.25 : 0.65 }}
            animate={{ opacity: 0 }}
            transition={{ duration: shouldReduceMotion ? 0.16 : 0.42 }}
            onAnimationComplete={() => setShowAddedHighlight(false)}
          />
        )}
      </motion.div>
    </div>
  );
}

function NewTabEdgeFeedback({
  newTabId,
  scrollerRef,
  shouldReduceMotion,
}: {
  newTabId: string | null;
  scrollerRef: RefObject<HTMLDivElement | null>;
  shouldReduceMotion: boolean;
}) {
  const sequenceRef = useRef(0);
  const [signal, setSignal] = useState<{
    tabId: string;
    sequence: number;
  } | null>(null);

  useLayoutEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller || !newTabId) return;
    const tab = getTabElement(scroller, newTabId);
    if (!tab || getTabScrollTarget(scroller, tab) === scroller.scrollLeft) {
      return;
    }

    sequenceRef.current += 1;
    setSignal({ tabId: newTabId, sequence: sequenceRef.current });
  }, [newTabId, scrollerRef]);

  if (!signal) return null;

  return (
    <motion.div
      key={`${signal.tabId}-${signal.sequence}`}
      aria-hidden
      data-new-tab-edge-feedback="true"
      className="pointer-events-none absolute top-4 bottom-1 right-0 z-20 w-8 rounded-r-lg bg-gradient-to-l from-primary/35 via-primary/10 to-transparent"
      initial={{
        opacity: shouldReduceMotion ? 0.45 : 0,
        x: shouldReduceMotion ? 0 : 4,
      }}
      animate={{
        opacity: shouldReduceMotion ? [0.45, 0] : [0, 0.8, 0],
        x: 0,
      }}
      transition={{
        opacity: {
          duration: shouldReduceMotion ? 0.2 : 0.45,
          times: shouldReduceMotion ? [0, 1] : [0, 0.18, 1],
          ease: "easeOut",
        },
        x: {
          duration: shouldReduceMotion ? 0 : 0.18,
          ease: TAB_ENTRY_EASE,
        },
      }}
      onAnimationComplete={() => {
        setSignal((current) =>
          current?.sequence === signal.sequence ? null : current,
        );
      }}
    />
  );
}

function NewTabButton() {
  const addTab = useTabStore((s) => s.addTab);
  const setActiveTab = useTabStore((s) => s.setActiveTab);

  const handleClick = () => {
    // New tab opens in the currently active workspace — tabs are scoped
    // per workspace, so there is no cross-workspace ambiguity to resolve.
    const activeSlug = useTabStore.getState().activeWorkspaceSlug;
    if (!activeSlug) return;
    const path = paths.workspace(activeSlug).issues();
    const tabId = addTab(path, "Issues", resolveRouteIcon(path));
    if (tabId) setActiveTab(tabId);
  };

  return (
    <button
      type="button"
      onClick={handleClick}
      aria-label="New tab"
      title="New tab"
      style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      className="mb-1 flex size-7 shrink-0 items-center justify-center self-end rounded-md text-muted-foreground/70 transition-colors hover:bg-muted/50 hover:text-muted-foreground"
    >
      <Plus className="size-3.5" />
    </button>
  );
}

export function TabBar() {
  const group = useActiveGroup();
  const moveTab = useTabStore((s) => s.moveTab);
  const activeWorkspaceSlug = useTabStore((s) => s.activeWorkspaceSlug);
  const shouldReduceMotion = useReducedMotion() ?? false;
  const tabScrollRef = useRef<HTMLDivElement>(null);
  const previousTabsRef = useRef<TabSnapshot | null>(null);
  const tabFadeStyle = useScrollFade(
    tabScrollRef,
    TAB_SCROLL_FADE_SIZE,
    "horizontal",
  );

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    }),
  );

  const tabs = group?.tabs ?? [];
  const activeTabId = group?.activeTabId ?? "";
  const tabIds = tabs.map((t) => t.id);
  const tabOrder = tabIds.join("\0");
  const tabLayoutKey = tabs
    .map((tab) => `${tab.id}:${tab.pinned ? "pinned" : "unpinned"}`)
    .join("\0");
  const addedTabIds = getAddedTabIds(
    previousTabsRef.current,
    activeWorkspaceSlug,
    tabIds,
  );
  const addedTabIdSet = new Set(addedTabIds);
  const newestTabId = addedTabIds.at(-1) ?? null;
  const backgroundAddedTabId =
    newestTabId && newestTabId !== activeTabId ? newestTabId : null;
  const pinnedCount = tabs.filter((t) => t.pinned).length;
  const unpinnedCount = tabs.length - pinnedCount;

  useLayoutEffect(() => {
    const currentTabIds = tabOrder ? tabOrder.split("\0") : [];
    const newlyAddedIds = getAddedTabIds(
      previousTabsRef.current,
      activeWorkspaceSlug,
      currentTabIds,
    );
    previousTabsRef.current = {
      workspaceSlug: activeWorkspaceSlug,
      ids: new Set(currentTabIds),
    };

    if (newlyAddedIds.length > 0) {
      if (newlyAddedIds.includes(activeTabId)) {
        keepTabVisible(
          tabScrollRef.current,
          activeTabId,
          shouldReduceMotion ? "auto" : "smooth",
        );
      }
      // Background additions intentionally preserve the user's current tab and
      // scroll position. NewTabEdgeFeedback handles the offscreen acknowledgement.
      return;
    }

    keepTabVisible(tabScrollRef.current);
  }, [
    activeWorkspaceSlug,
    activeTabId,
    shouldReduceMotion,
    tabLayoutKey,
    tabOrder,
  ]);

  useEffect(() => {
    const scroller = tabScrollRef.current;
    if (!scroller || typeof ResizeObserver === "undefined") return;

    // Sidebar and window resizing can hide the active tab without changing
    // activeTabId, so keep it visible as the strip's viewport changes.
    const resizeObserver = new ResizeObserver(() => keepTabVisible(scroller));
    resizeObserver.observe(scroller);
    return () => resizeObserver.disconnect();
  }, []);

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const from = tabs.findIndex((t) => t.id === active.id);
    const to = tabs.findIndex((t) => t.id === over.id);
    // The store clamps the destination to within the source tab's zone
    // (pinned vs unpinned), so this call is safe even when the user tries
    // to drag across the boundary — the tab will land at the boundary.
    if (from !== -1 && to !== -1) moveTab(from, to);
  };

  return (
    <div className="flex h-full w-full min-w-0 max-w-full items-center justify-start gap-0.5 px-2">
      <div className="relative flex h-full min-w-0 flex-1 items-center">
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          modifiers={[restrictToHorizontalAxis, restrictToParentElement]}
          onDragEnd={handleDragEnd}
        >
          {/* px-4 keeps the active tab's flares inside the scroller's clip
              (overflow clips at the padding box) and clears the content
              card's rounded top-left corner below the first tab. */}
          <div
            ref={tabScrollRef}
            data-tab-scroll-container
            className="no-scrollbar flex h-full min-w-0 flex-1 items-end overflow-x-auto overflow-y-hidden overscroll-x-contain px-4"
            style={tabFadeStyle}
          >
            <SortableContext items={tabIds} strategy={horizontalListSortingStrategy}>
              {tabs.map((tab, index) => {
                const previousTab = index > 0 ? tabs[index - 1] : null;
                return (
                  <Fragment key={tab.id}>
                    <SortableTabItem
                      tab={tab}
                      isActive={tab.id === activeTabId}
                      isOnly={tabs.length === 1}
                      canCloseOthers={tabs.some(
                        (candidate) => candidate.id !== tab.id && !candidate.pinned,
                      )}
                      isNew={addedTabIdSet.has(tab.id)}
                      shouldReduceMotion={shouldReduceMotion}
                      showSeparator={
                        !!previousTab &&
                        tab.id !== activeTabId &&
                        previousTab.id !== activeTabId &&
                        // the pinned-zone divider already separates this pair
                        !(previousTab.pinned && !tab.pinned)
                      }
                    />
                    {tab.pinned &&
                      index === pinnedCount - 1 &&
                      unpinnedCount > 0 && (
                        <div
                          aria-hidden
                          className="mx-1 mb-2.5 h-4 w-px shrink-0 self-end bg-border"
                        />
                      )}
                  </Fragment>
                );
              })}
            </SortableContext>
          </div>
        </DndContext>
        <NewTabEdgeFeedback
          newTabId={backgroundAddedTabId}
          scrollerRef={tabScrollRef}
          shouldReduceMotion={shouldReduceMotion}
        />
      </div>
      {group && <NewTabButton />}
    </div>
  );
}
