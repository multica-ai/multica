import { Fragment } from "react";
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

function SortableTabItem({
  tab,
  isActive,
  isOnly,
}: {
  tab: Tab;
  isActive: boolean;
  /**
   * True iff this is the only tab in the workspace. Hiding X on the last
   * tab matches existing behavior and avoids the surprise of the store's
   * last-tab reseed kicking in. Pinned tabs always hide X (RFC §3 D3c).
   */
  isOnly: boolean;
}) {
  const setActiveTab = useTabStore((s) => s.setActiveTab);
  const closeTab = useTabStore((s) => s.closeTab);
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
    zIndex: isDragging ? 10 : undefined,
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

  const tabButton = (
    <button
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      onClick={handleClick}
      aria-label={tab.pinned ? `${tab.title} (pinned)` : tab.title}
      title={tab.pinned ? `${tab.title} (pinned)` : undefined}
      className={cn(
        "group flex h-7 w-40 items-center gap-1.5 rounded-md px-2 text-xs transition-colors",
        "select-none cursor-default",
        isActive
          ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
          : "bg-sidebar-accent/50 text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
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
      </ContextMenuContent>
    </ContextMenu>
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
      onClick={handleClick}
      style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground/70 transition-colors hover:bg-muted/50 hover:text-muted-foreground"
    >
      <Plus className="size-3.5" />
    </button>
  );
}

export function TabBar() {
  const group = useActiveGroup();
  const moveTab = useTabStore((s) => s.moveTab);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    }),
  );

  const tabs = group?.tabs ?? [];
  const activeTabId = group?.activeTabId ?? "";
  const tabIds = tabs.map((t) => t.id);
  const pinnedCount = tabs.filter((t) => t.pinned).length;
  const unpinnedCount = tabs.length - pinnedCount;

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
    <div className="flex h-full items-center gap-0.5 px-2 justify-start">
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={[restrictToHorizontalAxis, restrictToParentElement]}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={tabIds} strategy={horizontalListSortingStrategy}>
          {tabs.map((tab, index) => (
            <Fragment key={tab.id}>
              <SortableTabItem
                tab={tab}
                isActive={tab.id === activeTabId}
                isOnly={tabs.length === 1}
              />
              {tab.pinned &&
                index === pinnedCount - 1 &&
                unpinnedCount > 0 && (
                  <div
                    aria-hidden
                    className="mx-1 h-4 w-px bg-border"
                  />
                )}
            </Fragment>
          ))}
        </SortableContext>
      </DndContext>
      {group && <NewTabButton />}
    </div>
  );
}
