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
import { cn } from "@multica/ui/lib/utils";
import { useTabStore, useActiveGroup, resolveRouteIcon, type Tab } from "@/stores/tab-store";
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

function SortableTabItem({ tab, isActive, isOnly }: { tab: Tab; isActive: boolean; isOnly: boolean }) {
  const setActiveTab = useTabStore((s) => s.setActiveTab);
  const closeTab = useTabStore((s) => s.closeTab);

  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: tab.id });

  const Icon = TAB_ICONS[tab.icon];

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

  const stopDragOnClose = (e: React.PointerEvent) => {
    e.stopPropagation();
  };

  return (
    <button
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      onClick={handleClick}
      className={cn(
        "group flex h-7 w-40 items-center gap-1.5 rounded-md px-2 text-xs transition-colors",
        "select-none cursor-default",
        isActive
          ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
          : "bg-sidebar-accent/50 text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        isDragging && "opacity-60",
      )}
    >
      {Icon && <Icon className="size-3.5 shrink-0" />}
      <span
        className="min-w-0 flex-1 overflow-hidden whitespace-nowrap text-left"
        style={{
          maskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
          WebkitMaskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
        }}
      >
        {tab.title}
      </span>
      {!isOnly && (
        <span
          onClick={handleClose}
          onPointerDown={stopDragOnClose}
          className="hidden size-3.5 shrink-0 items-center justify-center rounded-sm text-muted-foreground transition-colors group-hover:flex hover:bg-muted-foreground/20 hover:text-foreground"
        >
          <X className="size-2.5" />
        </span>
      )}
    </button>
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

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const from = tabs.findIndex((t) => t.id === active.id);
    const to = tabs.findIndex((t) => t.id === over.id);
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
          {tabs.map((tab) => (
            <SortableTabItem
              key={tab.id}
              tab={tab}
              isActive={tab.id === activeTabId}
              isOnly={tabs.length === 1}
            />
          ))}
        </SortableContext>
      </DndContext>
      {group && <NewTabButton />}
    </div>
  );
}
