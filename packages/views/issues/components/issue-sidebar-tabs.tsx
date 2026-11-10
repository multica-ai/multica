"use client";

import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Tabs as TabsPrimitive } from "@base-ui/react/tabs";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCenter,
  pointerWithin,
  useDroppable,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
  type UniqueIdentifier,
} from "@dnd-kit/core";
import { SortableContext, horizontalListSortingStrategy, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useDefaultLayout } from "react-resizable-panels";
import { ListChevronsDownUp, MoreHorizontal, RotateCcw, Rows3 } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@multica/ui/components/ui/resizable";
import { cn } from "@multica/ui/lib/utils";
import {
  dropIssueSidebarTab,
  groupOfTab,
  mergeIssueSidebarGroups,
  moveIssueSidebarTab,
  selectIssueSidebarLayout,
  selectIssueSidebarTab,
  splitIssueSidebarTab,
  useIssueSidebarLayoutStore,
  type IssueSidebarDropTarget,
  type IssueSidebarGroup,
  type IssueSidebarLayout,
  type IssueSidebarTab,
} from "@multica/core/issues/stores";
import { useT } from "../../i18n";
import { IssueSidebarStack } from "./issue-sidebar-section";

type TabLabels = Record<IssueSidebarTab, string>;
type TabCounts = Partial<Record<IssueSidebarTab, number>>;

type DropData =
  | { type: "tab"; tab: IssueSidebarTab; group: string }
  | { type: "strip"; group: string }
  | { type: "zone"; group: string; edge: "above" | "below" };

interface DragState {
  tab: IssueSidebarTab;
  /** The group the tab was picked up from. */
  from: string;
  /** The layout as previewed while dragging; tabs may leave a group empty. */
  draft: IssueSidebarLayout;
}

const TAB_CLASS =
  "inline-flex h-7 min-w-0 items-center gap-1.5 rounded-button-sm px-2.5 text-label whitespace-nowrap text-muted-foreground outline-none transition-colors hover:bg-accent/50 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring data-active:bg-secondary data-active:font-medium data-active:text-secondary-foreground data-active:hover:bg-secondary";

function useTabLabels(): TabLabels {
  const { t } = useT("issues");
  return {
    overview: t(($) => $.detail.sidebar_tabs.tab_overview),
    delivery: t(($) => $.detail.sidebar_tabs.tab_delivery),
    runs: t(($) => $.detail.sidebar_tabs.tab_runs),
  };
}

function TabLabel({ label, count }: { label: string; count?: number }) {
  return (
    <>
      <span className="truncate">{label}</span>
      {count ? (
        <span className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium tabular-nums text-muted-foreground in-data-active:bg-background">
          {count}
        </span>
      ) : null}
    </>
  );
}

function TabPanel({ tab, children }: { tab: IssueSidebarTab; children: ReactNode }) {
  const { t } = useT("issues");
  return (
    // Each panel scrolls on its own. Being positioned, it is also the
    // containing block for its sections' absolutely placed `sr-only` labels.
    <TabsPrimitive.Panel
      value={tab}
      keepMounted
      className="absolute inset-0 overflow-x-hidden overflow-y-auto px-2 pt-3 pb-4 outline-none [scrollbar-gutter:stable] focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
    >
      <IssueSidebarStack className="peer">{children}</IssueSidebarStack>
      <p className="hidden px-4 text-caption text-muted-foreground peer-empty:block">
        {t(($) => $.detail.sidebar_tabs.empty)}
      </p>
    </TabsPrimitive.Panel>
  );
}

/**
 * The issue sidebar as tabs. Every tab stays mounted while hidden, so its
 * sections keep their state and can be scrolled to by id. Tabs are dragged
 * to reorder, onto another group's tab strip to move, or onto the top or
 * bottom half of a group to split it; the layout menu does the same without
 * dragging. The layout is the user's, shared by every issue on this device.
 */
export function IssueSidebarTabs({
  panels,
  counts,
  compact = false,
}: {
  panels: Record<IssueSidebarTab, ReactNode>;
  counts?: TabCounts;
  /** One fixed group with no dragging, for the mobile sheet. */
  compact?: boolean;
}) {
  const layout = useIssueSidebarLayoutStore(selectIssueSidebarLayout);
  return compact ? (
    <CompactTabs layout={layout} panels={panels} counts={counts} />
  ) : (
    <GroupedTabs layout={layout} panels={panels} counts={counts} />
  );
}

function CompactTabs({
  layout,
  panels,
  counts,
}: {
  layout: IssueSidebarLayout;
  panels: Record<IssueSidebarTab, ReactNode>;
  counts?: TabCounts;
}) {
  const labels = useTabLabels();
  const selectTab = useIssueSidebarLayoutStore((s) => s.selectTab);
  const reveal = useIssueSidebarLayoutStore((s) => s.reveal);
  const tabs = useMemo(() => layout.flatMap((group) => group.tabs), [layout]);
  const [active, setActive] = useState<IssueSidebarTab>(() => reveal?.tab ?? layout[0]!.active);

  useEffect(() => {
    if (reveal) setActive(reveal.tab);
  }, [reveal]);

  return (
    <TabsPrimitive.Root
      value={active}
      onValueChange={(value: IssueSidebarTab) => {
        setActive(value);
        selectTab(value);
      }}
      className="flex h-full min-h-0 flex-col"
    >
      <div className="flex h-12 shrink-0 items-center border-b px-2">
        <TabsPrimitive.List className="flex min-w-0 flex-1 items-center gap-0.5">
          {tabs.map((tab) => (
            <TabsPrimitive.Tab key={tab} value={tab} className={TAB_CLASS}>
              <TabLabel label={labels[tab]} count={counts?.[tab]} />
            </TabsPrimitive.Tab>
          ))}
        </TabsPrimitive.List>
      </div>
      <div className="relative min-h-0 flex-1">
        {tabs.map((tab) => (
          <TabPanel key={tab} tab={tab}>
            {panels[tab]}
          </TabPanel>
        ))}
      </div>
    </TabsPrimitive.Root>
  );
}

function dropDataOf(data: unknown): DropData | undefined {
  return data && typeof data === "object" && "type" in data ? (data as DropData) : undefined;
}

/**
 * A tab under the pointer wins; then the strip around it, resolved to the
 * nearest tab so a drop between tabs lands between them (or appends, past the
 * last one); then the split zones over a group's content.
 */
const collisionDetection: CollisionDetection = (args) => {
  const hits = pointerWithin(args);
  const dataOf = (id: UniqueIdentifier) => dropDataOf(args.droppableContainers.find((c) => c.id === id)?.data.current);
  const tabHit = hits.find((hit) => dataOf(hit.id)?.type === "tab");
  if (tabHit) return [tabHit];
  const stripHit = hits.find((hit) => dataOf(hit.id)?.type === "strip");
  if (stripHit) {
    const group = (dataOf(stripHit.id) as Extract<DropData, { type: "strip" }>).group;
    const tabs = args.droppableContainers.filter((c) => {
      const data = dropDataOf(c.data.current);
      return data?.type === "tab" && data.group === group;
    });
    const right = Math.max(...tabs.map((c) => args.droppableRects.get(c.id)?.right ?? -Infinity));
    if (tabs.length === 0 || (args.pointerCoordinates && args.pointerCoordinates.x > right)) return [stripHit];
    return closestCenter({ ...args, droppableContainers: tabs }).slice(0, 1);
  }
  const zoneHit = hits.find((hit) => dataOf(hit.id)?.type === "zone");
  return zoneHit ? [zoneHit] : [];
};

function toDropTarget(data: DropData): IssueSidebarDropTarget {
  if (data.type === "tab") return { type: "tab", tab: data.tab };
  if (data.type === "strip") return { type: "strip", group: data.group };
  return { type: "zone", group: data.group, edge: data.edge };
}

function GroupedTabs({
  layout,
  panels,
  counts,
}: {
  layout: IssueSidebarLayout;
  panels: Record<IssueSidebarTab, ReactNode>;
  counts?: TabCounts;
}) {
  const labels = useTabLabels();
  const setLayout = useIssueSidebarLayoutStore((s) => s.setLayout);
  const selectTab = useIssueSidebarLayoutStore((s) => s.selectTab);
  const [drag, setDrag] = useState<DragState | null>(null);
  const dragRef = useRef<DragState | null>(null);
  dragRef.current = drag;
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const groupIds = layout.map((group) => group.id);
  const groupKey = groupIds.join("-");
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: `multica_issue_sidebar_groups:${groupKey}`,
    panelIds: groupIds,
  });

  const onDragStart = ({ active }: DragStartEvent) => {
    const tab = active.id as IssueSidebarTab;
    const from = groupOfTab(layout, tab);
    if (from) setDrag({ tab, from: from.id, draft: layout });
  };

  // Crossing into another group's strip moves the tab there in the preview;
  // within one strip, sortable shifts the tabs itself. Over a split zone, or
  // nowhere, the tab goes back to its own strip so only the zone previews.
  const onDragOver = ({ over }: DragOverEvent) => {
    const data = dropDataOf(over?.data.current);
    if (!data || data.type === "zone") {
      setDrag((current) => (current && current.draft !== layout ? { ...current, draft: layout } : current));
      return;
    }
    setDrag((current) => {
      if (!current) return current;
      const source = groupOfTab(current.draft, current.tab);
      const target = data.type === "tab" ? groupOfTab(current.draft, data.tab) : current.draft.find((g) => g.id === data.group);
      if (!source || !target || source.id === target.id) return current;
      const index = data.type === "tab" ? target.tabs.indexOf(data.tab) : target.tabs.length;
      return { ...current, draft: moveIssueSidebarTab(current.draft, current.tab, target.id, index) };
    });
  };

  const onDragEnd = ({ over }: DragEndEvent) => {
    const current = dragRef.current;
    setDrag(null);
    const data = dropDataOf(over?.data.current);
    if (!current || !data) return;
    setLayout(selectIssueSidebarTab(dropIssueSidebarTab(current.draft, current.tab, toDropTarget(data)), current.tab));
  };

  const draftSource = drag ? groupOfTab(drag.draft, drag.tab) : undefined;

  const renderGroup = (group: IssueSidebarGroup, index: number) => {
    // Strips preview the drag; panels stay where they are until the drop.
    const strip = drag?.draft.find((g) => g.id === group.id) ?? group;
    const canSplit = !!drag && !(draftSource?.id === group.id && draftSource.tabs.length === 1);
    return (
      <TabsPrimitive.Root
        value={group.active}
        onValueChange={(value: IssueSidebarTab) => {
          if (!dragRef.current) selectTab(value);
        }}
        className={cn("flex h-full min-h-0 flex-col", index > 0 && "border-t")}
      >
        <TabStrip
          group={strip}
          top={index === 0}
          labels={labels}
          counts={counts}
          receiving={!!drag && strip.tabs.includes(drag.tab) && group.id !== drag.from}
          menu={<LayoutMenu layout={layout} group={group} labels={labels} />}
        />
        <div className={cn("relative min-h-0 flex-1 transition-opacity", strip.tabs.length === 0 && "opacity-40")}>
          {group.tabs.map((tab) => (
            <TabPanel key={tab} tab={tab}>
              {panels[tab]}
            </TabPanel>
          ))}
          {canSplit && (
            <>
              <SplitZone group={group.id} edge="above" />
              <SplitZone group={group.id} edge="below" />
            </>
          )}
        </div>
      </TabsPrimitive.Root>
    );
  };

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={onDragStart}
      onDragOver={onDragOver}
      onDragEnd={onDragEnd}
      onDragCancel={() => setDrag(null)}
    >
      {layout.length === 1 ? (
        renderGroup(layout[0]!, 0)
      ) : (
        <ResizablePanelGroup key={groupKey} orientation="vertical" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
          {layout.map((group, index) => (
            <Fragment key={group.id}>
              {index > 0 && <ResizableHandle />}
              <ResizablePanel id={group.id} minSize={96}>
                {renderGroup(group, index)}
              </ResizablePanel>
            </Fragment>
          ))}
        </ResizablePanelGroup>
      )}
      {/* The sidebar slides in under a transform, which would offset a fixed
          overlay rendered inside it. */}
      {typeof document !== "undefined" &&
        createPortal(
          <DragOverlay dropAnimation={null}>
            {drag ? (
              <span className={cn(TAB_CLASS, "bg-popover font-medium text-foreground shadow-[var(--menu-shadow)] ring-1 ring-surface-border")}>
                <TabLabel label={labels[drag.tab]} count={counts?.[drag.tab]} />
              </span>
            ) : null}
          </DragOverlay>,
          document.body,
        )}
    </DndContext>
  );
}

function TabStrip({
  group,
  top,
  labels,
  counts,
  receiving,
  menu,
}: {
  group: IssueSidebarGroup;
  top: boolean;
  labels: TabLabels;
  counts?: TabCounts;
  /** A tab from another group is being dragged into this strip. */
  receiving: boolean;
  menu: ReactNode;
}) {
  const { setNodeRef } = useDroppable({ id: `strip:${group.id}`, data: { type: "strip", group: group.id } satisfies DropData });
  return (
    // The first strip shares the 48px band of the page header beside it.
    <div
      ref={setNodeRef}
      className={cn("flex shrink-0 items-center gap-1 border-b px-2 transition-colors", top ? "h-12" : "h-10", receiving && "bg-brand/5")}
    >
      <SortableContext items={group.tabs} strategy={horizontalListSortingStrategy}>
        <TabsPrimitive.List className="flex min-w-0 flex-1 items-center gap-0.5">
          {group.tabs.map((tab) => (
            <SortableTab key={tab} tab={tab} group={group.id} label={labels[tab]} count={counts?.[tab]} />
          ))}
        </TabsPrimitive.List>
      </SortableContext>
      {menu}
    </div>
  );
}

function SortableTab({ tab, group, label, count }: { tab: IssueSidebarTab; group: string; label: string; count?: number }) {
  const { setNodeRef, listeners, transform, transition, isDragging } = useSortable({
    id: tab,
    data: { type: "tab", tab, group } satisfies DropData,
  });
  return (
    <TabsPrimitive.Tab
      ref={setNodeRef}
      value={tab}
      {...listeners}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(TAB_CLASS, isDragging && "opacity-40")}
    >
      <TabLabel label={label} count={count} />
    </TabsPrimitive.Tab>
  );
}

function SplitZone({ group, edge }: { group: string; edge: "above" | "below" }) {
  const { t } = useT("issues");
  const { setNodeRef, isOver } = useDroppable({ id: `zone:${group}:${edge}`, data: { type: "zone", group, edge } satisfies DropData });
  return (
    <div ref={setNodeRef} className={cn("absolute inset-x-0 z-10 h-1/2 p-2", edge === "above" ? "top-0" : "bottom-0")}>
      {isOver && (
        <div className="flex h-full items-center justify-center rounded-lg bg-brand/10 ring-1 ring-brand/50 ring-inset">
          <span className="inline-flex items-center gap-1.5 rounded-md bg-popover px-2.5 py-1.5 text-caption font-medium text-brand shadow-[var(--menu-shadow)] ring-1 ring-surface-border">
            <Rows3 className="size-3.5" />
            {edge === "above" ? t(($) => $.detail.sidebar_tabs.drop_above) : t(($) => $.detail.sidebar_tabs.drop_below)}
          </span>
        </div>
      )}
    </div>
  );
}

function LayoutMenu({ layout, group, labels }: { layout: IssueSidebarLayout; group: IssueSidebarGroup; labels: TabLabels }) {
  const { t } = useT("issues");
  const customized = useIssueSidebarLayoutStore((s) => s.layout !== null);
  const setLayout = useIssueSidebarLayoutStore((s) => s.setLayout);
  const reset = useIssueSidebarLayoutStore((s) => s.reset);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton variant="ghost" size="icon-xs" className="text-muted-foreground" label={t(($) => $.detail.sidebar_tabs.layout_menu)}>
            <MoreHorizontal />
          </IconButton>
        }
      />
      <DropdownMenuContent align="end" className="w-max max-w-(--available-width)">
        <DropdownMenuItem
          disabled={group.tabs.length < 2}
          onClick={() => setLayout(splitIssueSidebarTab(layout, group.active, group.id, "below"))}
        >
          <Rows3 />
          {t(($) => $.detail.sidebar_tabs.split_below, { tab: labels[group.active] })}
        </DropdownMenuItem>
        <DropdownMenuItem disabled={layout.length < 2} onClick={() => setLayout(mergeIssueSidebarGroups(layout))}>
          <ListChevronsDownUp />
          {t(($) => $.detail.sidebar_tabs.merge_all)}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={!customized} onClick={reset}>
          <RotateCcw />
          {t(($) => $.detail.sidebar_tabs.reset)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
