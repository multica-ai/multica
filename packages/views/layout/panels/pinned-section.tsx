"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  closestCenter,
  type DragEndEvent,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, useSortable, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Folder, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { ApiError } from "@multica/core/api";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { pinListOptions } from "@multica/core/pins/queries";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useDeletePin, useReorderPins } from "@multica/core/pins/mutations";
import { useIssueUnread } from "@multica/core/issues/unread";
import { useWorkspacePaths, type WorkspacePaths } from "@multica/core/paths";
import type { Issue, PinnedItem } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { IssueActionsContextMenu } from "../../issues/actions";
import { useStatusLabel } from "../../issues/utils/status-label";
import { useT } from "../../i18n";
import { PANEL_ROW_CLASS, PanelList, PanelSection, isPathWithin, panelRowStateClass } from "../panel-nav";
import {
  ISSUE_ROW_LINK_CLASS,
  IssueAgentBadge,
  IssueUnreadDot,
  SIDEBAR_SHIMMER_CLASS,
  SidebarIssueStatus,
  issueRowMenuOpenClass,
  useIssueAgentState,
} from "./sidebar-tree";

// Stable empty default: an inline `= []` on useQuery is a new reference every
// render while the query is disabled, which re-fires the sync effect below.
const EMPTY_PINS: PinnedItem[] = [];
const PINNED_PREVIEW_LIMIT = 5;

function pinHref(p: WorkspacePaths, pin: PinnedItem): string {
  return pin.item_type === "issue" ? p.issueDetail(pin.item_id) : p.projectDetail(pin.item_id);
}

/**
 * Pin list state for the Issues panel. Lifted out of the section so the
 * panel can keep its plain rows unlit while a pinned item is the page on
 * screen.
 *
 * An issue pin matches by id, not by path: the issue route rewrites its UUID
 * URL to the `MUL-123` form, which a pin's href never equals.
 */
export function usePanelPins(
  wsId: string | undefined,
  userId: string | undefined,
  pathname: string,
  currentIssueId: string | null,
) {
  const p = useWorkspacePaths();
  const { data: allPins = EMPTY_PINS } = useQuery({
    ...pinListOptions(wsId ?? "", userId ?? ""),
    enabled: !!wsId && !!userId,
  });
  // View pins moved into the Views section's starred list; a reorder here
  // still carries them so the cache keeps them.
  const pinnedItems = useMemo(() => allPins.filter((pin) => pin.item_type !== "view"), [allPins]);
  const viewPins = useMemo(() => allPins.filter((pin) => pin.item_type === "view"), [allPins]);

  // Local presentational copy for drop-animation stability. Follows the query
  // at rest; frozen during a drag so a mid-drag cache write (our own
  // optimistic update, or a WS refetch) cannot reorder the DOM under dnd-kit
  // while its drop animation is still interpolating.
  const [localPinned, setLocalPinned] = useState<PinnedItem[]>(pinnedItems);
  const [localPinnedWsId, setLocalPinnedWsId] = useState<string | null>(wsId ?? null);
  const [expandedWsId, setExpandedWsId] = useState<string | null>(null);
  const isDraggingRef = useRef(false);
  useEffect(() => {
    if (!isDraggingRef.current) setLocalPinned(pinnedItems);
  }, [pinnedItems]);
  useEffect(() => {
    setLocalPinnedWsId(wsId ?? null);
  }, [wsId]);

  const visible = localPinnedWsId === (wsId ?? null) ? localPinned : EMPTY_PINS;
  const expanded = expandedWsId === (wsId ?? null);
  const displayed = expanded ? visible : visible.slice(0, PINNED_PREVIEW_LIMIT);
  const isPinActive = (pin: PinnedItem) =>
    pin.item_type === "issue"
      ? pin.item_id === currentIssueId
      : isPathWithin(pathname, pinHref(p, pin));
  // A collapsed section shows no rows, so it must not keep the tree unlit.
  const pinnedCollapsed = useSidebarTreeStore((s) => s.pinnedCollapsed);
  const isActivePinnedRoute = !pinnedCollapsed && displayed.some(isPinActive);

  return {
    visible,
    displayed,
    expanded,
    isActivePinnedRoute,
    isPinActive,
    setExpanded: (next: boolean) => setExpandedWsId(next ? wsId ?? null : null),
    setLocalPinned,
    isDraggingRef,
    viewPins,
  };
}

type PanelPins = ReturnType<typeof usePanelPins>;

export function PinnedSection({ pins, wsId }: { pins: PanelPins; wsId: string }) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const deletePin = useDeletePin();
  const reorderPins = useReorderPins();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const { visible, displayed, expanded, setExpanded, setLocalPinned, isDraggingRef, viewPins, isPinActive } = pins;
  const collapsed = useSidebarTreeStore((s) => s.pinnedCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setPinnedCollapsed);

  const handleDragStart = useCallback(() => {
    isDraggingRef.current = true;
  }, [isDraggingRef]);
  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      isDraggingRef.current = false;
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const oldIndex = visible.findIndex((pin) => pin.id === active.id);
      const newIndex = visible.findIndex((pin) => pin.id === over.id);
      if (oldIndex === -1 || newIndex === -1) return;
      const reordered = arrayMove(visible, oldIndex, newIndex);
      setLocalPinned(reordered);
      reorderPins.mutate([...reordered, ...viewPins]);
    },
    [isDraggingRef, reorderPins, setLocalPinned, viewPins, visible],
  );

  if (visible.length === 0) return null;

  return (
    <PanelSection
      label={t(($) => $.sidebar.pinned_label)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
    >
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
        <SortableContext items={displayed.map((pin) => pin.id)} strategy={verticalListSortingStrategy}>
          <PanelList>
            {displayed.map((pin) => (
              <PinRow
                key={pin.id}
                pin={pin}
                href={pinHref(p, pin)}
                active={isPinActive(pin)}
                onUnpin={() => deletePin.mutate({ itemType: pin.item_type, itemId: pin.item_id })}
                wsId={wsId}
              />
            ))}
          </PanelList>
        </SortableContext>
      </DndContext>
      {visible.length > PINNED_PREVIEW_LIMIT && (
        <button
          type="button"
          aria-expanded={expanded}
          className={cn(PANEL_ROW_CLASS, panelRowStateClass(false), "pl-[2.375rem] text-caption")}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded
            ? t(($) => $.sidebar.show_fewer_pins)
            : t(($) => $.sidebar.show_more_pins, { count: visible.length - PINNED_PREVIEW_LIMIT })}
        </button>
      )}
    </PanelSection>
  );
}

function SortablePinItem({
  pin,
  href,
  onUnpin,
  label,
  iconNode,
  active,
  running = false,
  unread = false,
  trailing,
  onNavigate,
  issue,
}: {
  pin: PinnedItem;
  href: string;
  onUnpin: () => void;
  label: string;
  iconNode: React.ReactNode;
  active: boolean;
  /** Shimmers the label while an agent works on the pinned issue. */
  running?: boolean;
  /** Brightens the label while the issue has an unread agent result. */
  unread?: boolean;
  trailing?: React.ReactNode;
  /** Runs on a real click (not a drag-release) before navigation. */
  onNavigate?: () => void;
  /** A pinned issue's row opens the issue context menu on right-click. */
  issue?: Issue;
}) {
  const { t } = useT("layout");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: pin.id });
  const wasDragged = useRef(false);

  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);

  const row = (
    <div
      className={cn(
        PANEL_ROW_CLASS,
        panelRowStateClass(active),
        issue && issueRowMenuOpenClass(active),
        "relative",
        trailing ? "pr-2" : "pr-8",
        isDragging && "pointer-events-none",
      )}
    >
      {iconNode}
      <AppLink
        href={href}
        newTabTitle={label}
        draggable={false}
        aria-current={active ? "page" : undefined}
        onClick={(event) => {
          if (wasDragged.current) {
            wasDragged.current = false;
            event.preventDefault();
            return;
          }
          onNavigate?.();
        }}
        className={cn(ISSUE_ROW_LINK_CLASS, "gap-2.5")}
      >
        <ShimmerText
          active={running}
          className={cn("min-w-0 flex-1 truncate", SIDEBAR_SHIMMER_CLASS, unread && "text-foreground")}
        >
          {label}
        </ShimmerText>
        {/* Holds the same right-edge slot as a tree row's avatars, and yields
            it to the unpin button on hover. */}
        {trailing && (
          <span className="flex shrink-0 items-center group-hover/pin:invisible group-focus-within/pin:invisible">
            {trailing}
          </span>
        )}
      </AppLink>
    </div>
  );

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn("group/pin relative", isDragging && "opacity-30")}
      {...attributes}
      {...listeners}
    >
      {issue ? <IssueActionsContextMenu issue={issue}>{row}</IssueActionsContextMenu> : row}
      <TooltipHint label={t(($) => $.sidebar.unpin_tooltip)}>
        <button
          type="button"
          aria-label={t(($) => $.sidebar.unpin_tooltip)}
          className="absolute top-1/2 right-1.5 hidden size-5 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground group-hover/pin:flex group-focus-within/pin:flex hover:bg-surface-selected hover:text-foreground"
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onUnpin();
          }}
        >
          <X className="size-3" />
        </button>
      </TooltipHint>
    </li>
  );
}

/**
 * Resolves a pin's display data (label + status/icon) from the issue /
 * project / view detail cache. All three queries are declared unconditionally
 * with `enabled` gates so the hook order stays stable regardless of
 * `pin.item_type`.
 *
 * Loading renders a flat skeleton so the panel height doesn't jump. A missing
 * item (deleted / 404) renders nothing and unpins itself.
 */
function PinRow({
  pin,
  href,
  active,
  onUnpin,
  wsId,
}: {
  pin: PinnedItem;
  href: string;
  active: boolean;
  onUnpin: () => void;
  wsId: string;
}) {
  const isIssue = pin.item_type === "issue";
  const agents = useIssueAgentState(wsId, isIssue ? pin.item_id : "");
  const unread = useIssueUnread(wsId, isIssue ? pin.item_id : "");
  const statusCatalog = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);
  const issueQuery = useQuery({ ...issueDetailOptions(wsId, pin.item_id), enabled: isIssue });
  const projectQuery = useQuery({
    ...projectDetailOptions(wsId, pin.item_id),
    enabled: pin.item_type === "project",
  });

  const triggeredRef = useRef(false);
  useEffect(() => {
    const err = isIssue ? issueQuery.error : projectQuery.error;
    if (err instanceof ApiError && err.status === 404 && !triggeredRef.current) {
      triggeredRef.current = true;
      onUnpin();
    }
  }, [isIssue, issueQuery.error, onUnpin, projectQuery.error]);

  if (isIssue) {
    if (issueQuery.isPending) return <PinSkeleton />;
    if (issueQuery.isError || !issueQuery.data) return null;
    const issue = issueQuery.data;
    return (
      <SortablePinItem
        pin={pin}
        href={href}
        onUnpin={onUnpin}
        label={issue.title}
        active={active}
        running={!!agents?.running}
        unread={!agents && !!unread}
        trailing={agents ? <IssueAgentBadge agents={agents} /> : unread && <IssueUnreadDot unread={unread} />}
        issue={issue}
        iconNode={
          <SidebarIssueStatus
            issue={issue}
            color={statusCatalog.colorOf(issue.status)}
            icon={statusCatalog.iconOf(issue.status)}
            label={statusLabel(issue.status)}
            className="size-3.5"
          />
        }
      />
    );
  }

  if (projectQuery.isPending) return <PinSkeleton />;
  if (projectQuery.isError || !projectQuery.data) return null;
  const project = projectQuery.data;
  return (
    <SortablePinItem
      pin={pin}
      href={href}
      onUnpin={onUnpin}
      label={project.title}
      active={active}
      iconNode={<Folder className="size-4 shrink-0" />}
    />
  );
}

function PinSkeleton() {
  return (
    <li className="flex h-8 items-center gap-2.5 px-3">
      <div className="size-3.5 shrink-0 rounded-sm bg-muted" />
      <div className="h-3 w-24 rounded-xs bg-muted" />
    </li>
  );
}
