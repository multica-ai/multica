"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { AppLink, useNavigation } from "../navigation";
import { HelpLauncher } from "./help-launcher";
import { JoinDiscordCard } from "./join-discord-card";
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  closestCenter,
  MeasuringStrategy,
  type DragEndEvent,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, useSortable, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  Inbox,
  MessageSquare,
  ListTodo,
  Bot,
  Monitor,
  ChevronDown,
  ChevronRight,
  Settings,
  LogOut,
  Plus,
  Check,
  BookOpenText,
  SquarePen,
  CircleUser,
  FolderKanban,
  BarChart3,
  X,
  Zap,
  Users,
} from "lucide-react";
import { WorkspaceAvatar } from "../workspace/workspace-avatar";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from "@multica/ui/components/ui/collapsible";
import { CappedNumberFlow } from "@multica/ui/components/ui/number-flow";
import { StatusIcon } from "../issues/components/status-icon";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores/create-mode-store";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@multica/ui/components/ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths, paths } from "@multica/core/paths";
import { workspaceListOptions, myInvitationListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { inboxKeys, deduplicateInboxItems, inboxUnreadSummaryOptions, hasOtherWorkspaceUnread, unreadWorkspaceIds } from "@multica/core/inbox/queries";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { countUnreadChatMessages } from "@multica/core/chat/unread";
import { useChatStore } from "@multica/core/chat";
import { api, ApiError } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import { useConfigStore } from "@multica/core/config";
import { pinListOptions } from "@multica/core/pins/queries";
import { useDeletePin, useReorderPins } from "@multica/core/pins/mutations";
import { mySpaceListOptions } from "@multica/core/spaces/queries";
import { useUpdateSpaceMembership } from "@multica/core/spaces/mutations";
import { useSidebarStore } from "@multica/core/layout/sidebar-store";
import { SpaceIcon } from "../spaces/components/space-icon";
import type { Space } from "@multica/core/types";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import type { PinnedItem } from "@multica/core/types";
import { useLogout } from "../auth";
import { ProjectIcon } from "../projects/components/project-icon";
import { useT } from "../i18n";
import {
  useShortcut,
} from "@multica/core/shortcuts";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { useAppForeground } from "../common/use-app-foreground";

// Top-level nav items stay active when the user is on a child route
// (e.g. "Projects" stays lit on /:slug/projects/:id). Pinned items keep
// strict equality elsewhere — a pinned project shouldn't highlight on
// sub-pages of itself.
function isNavActive(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(href + "/");
}

// Stable empty arrays for query defaults. Using an inline `= []` default on
// `useQuery` creates a new array reference on every render when `data` is
// undefined (e.g. query disabled or loading) — which in turn breaks any
// `useEffect`/`useMemo` that depends on the value, and can trigger infinite
// re-render loops when the effect itself calls `setState`.
const EMPTY_PINS: PinnedItem[] = [];
const EMPTY_WORKSPACES: Awaited<ReturnType<typeof api.listWorkspaces>> = [];
const EMPTY_INVITATIONS: Awaited<ReturnType<typeof api.listMyInvitations>> = [];
const EMPTY_INBOX: Awaited<ReturnType<typeof api.listInbox>> = [];
const EMPTY_INBOX_SUMMARY: Awaited<ReturnType<typeof api.getInboxUnreadSummary>> = [];

// Nav items reference WorkspacePaths method names so they can be resolved
// against the current workspace slug at render time (see AppSidebar body).
// Only parameterless paths are valid nav destinations.
type NavKey =
  | "inbox"
  | "chat"
  | "myIssues"
  | "projects"
  | "autopilots"
  | "agents"
  | "squads"
  | "usage"
  | "runtimes"
  | "skills"
  | "settings";

// Static schema (key + icon) — labels resolved at render via useT("layout").
type NavLabelKey =
  | "inbox"
  | "chat"
  | "my_issues"
  | "spaces"
  | "projects"
  | "autopilots"
  | "agents"
  | "squads"
  | "usage"
  | "runtimes"
  | "skills"
  | "settings";

// Re-measure droppable rects during a space drag: groups collapse on drag
// start, so the rects cached at gesture start are immediately stale.
const spaceDragMeasuring = { droppable: { strategy: MeasuringStrategy.Always } };

const personalNav: { key: NavKey; labelKey: NavLabelKey; icon: typeof Inbox }[] = [
  { key: "inbox", labelKey: "inbox", icon: Inbox },
  { key: "chat", labelKey: "chat", icon: MessageSquare },
  { key: "myIssues", labelKey: "my_issues", icon: CircleUser },
];

// The workspace-wide issues list left the nav with the space rollout: issues
// live under their space (Spaces section below) or under My Issues. Space
// management lives on each space's detail page (the /spaces overview was cut
// from v1). Autopilots are space-scoped like issues (space_id NOT NULL,
// their output lands in their space), so they live under each space below —
// no global entry, same rationale as the removed workspace-wide Issues.
const workspaceNav: { key: NavKey; labelKey: NavLabelKey; icon: typeof Inbox }[] = [
  { key: "projects", labelKey: "projects", icon: FolderKanban },
  { key: "agents", labelKey: "agents", icon: Bot },
  { key: "squads", labelKey: "squads", icon: Users },
];

// Rendered as plain rows alongside workspaceNav (see the Workspace group
// below) — used to sit behind a "More" dropdown, now always visible.
const moreNav: { key: NavKey; labelKey: NavLabelKey; icon: typeof Inbox }[] = [
  { key: "skills", labelKey: "skills", icon: BookOpenText },
  { key: "usage", labelKey: "usage", icon: BarChart3 },
  { key: "runtimes", labelKey: "runtimes", icon: Monitor },
];

// Per-space children under the Spaces group. Hrefs are built per space key.
const spaceChildNav = [
  { pathKey: "spaceIssues", labelKey: "issues", icon: ListTodo },
  { pathKey: "spaceProjects", labelKey: "projects", icon: FolderKanban },
  { pathKey: "spaceAutopilots", labelKey: "autopilots", icon: Zap },
] as const;

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
}

// Controlled collapse state persisted per workspace (sidebar-store). Absent
// key = expanded, so new groups (and fresh workspaces) start open.
function useGroupCollapse(key: string) {
  const collapsed = useSidebarStore((s) => s.collapsed[key] === true);
  const setGroupCollapsed = useSidebarStore((s) => s.setGroupCollapsed);
  const onOpenChange = useCallback(
    (open: boolean) => setGroupCollapsed(key, !open),
    [key, setGroupCollapsed],
  );
  return { open: !collapsed, onOpenChange };
}

// One space's collapsible nav group inside the Spaces section: draggable via
// the same dnd-kit setup the Pinned section uses (PointerSensor distance 5,
// so plain clicks still toggle the collapse), children are the space's
// surfaces addressed by space key.
function SortableSpaceGroup({
  space,
  pathname,
  buildHref,
  detailHref,
  forceCollapsed,
}: {
  space: Space;
  pathname: string;
  buildHref: (pathKey: (typeof spaceChildNav)[number]["pathKey"], spaceKey: string) => string;
  detailHref: string;
  /** True while any space drag is in flight: every group renders collapsed so
      all sortable items are the same height — variable-height blocks make
      dnd-kit's rect math (and thus the whole gesture) feel broken. */
  forceCollapsed: boolean;
}) {
  const { t } = useT("layout");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: space.id });
  const collapse = useGroupCollapse(`space:${space.id}`);
  // Same trap the pinned rows hit: the click that ends a drag would follow
  // the row's link and reload the page. Mirror SortablePinItem's guard —
  // remember the drag and swallow exactly that one click.
  const wasDragged = useRef(false);
  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);
  // The row itself is the space's home (detail page). It stays highlighted
  // anywhere inside the space (issues/projects/autopilots too) so the sidebar
  // always shows which space you're in.
  const isSpaceActive = isNavActive(pathname, detailHref);
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(isDragging && "opacity-30")}
    >
      <Collapsible open={collapse.open && !forceCollapsed} onOpenChange={collapse.onOpenChange}>
        {/* The whole row navigates to the space page (and expands the group —
            navigation is context). The chevron sits right after the name like
            every other group label — always visible, secondary color, primary
            only when hovered (the one independently-clickable element: it
            toggles the collapse without navigating). Drag listeners live on
            the row; the 5px activation distance keeps clicks working. */}
        <div {...attributes} {...listeners}>
          <SidebarGroupLabel
            render={<AppLink href={detailHref} draggable={false} />}
            onClick={(event) => {
              if (wasDragged.current) {
                wasDragged.current = false;
                event.preventDefault();
                return;
              }
              collapse.onOpenChange(true);
            }}
            className={cn(
              "w-full cursor-pointer gap-1.5 hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground",
              isSpaceActive && "bg-sidebar-accent text-sidebar-accent-foreground",
              isDragging && "pointer-events-none",
            )}
          >
            <SpaceIcon space={space} className="size-3.5" />
            <span className="truncate">{space.name}</span>
            <button
              type="button"
              onClick={(e) => {
                // Toggle only — never follow the surrounding link.
                e.preventDefault();
                e.stopPropagation();
                collapse.onOpenChange(!collapse.open);
              }}
              className="-ml-1 flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground hover:bg-sidebar-accent hover:text-foreground"
            >
              <ChevronRight
                className={cn(
                  "!size-3 stroke-[2.5] transition-transform duration-200",
                  collapse.open && "rotate-90",
                )}
              />
            </button>
          </SidebarGroupLabel>
        </div>
        <CollapsibleContent>
          {/* pt-0.5 breathes the children away from their space row; the
              group container's gap-0.5 covers the space below. pl-5 lands
              child icons exactly under the space name's text start. */}
          <SidebarMenu className="gap-0.5 pt-0.5 pl-5">
            {spaceChildNav.map((child) => {
              const href = buildHref(child.pathKey, space.key);
              const isActive = isNavActive(pathname, href);
              return (
                <SidebarMenuItem key={child.pathKey}>
                  <SidebarMenuButton
                    isActive={isActive}
                    render={<AppLink href={href} />}
                    className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                  >
                    <child.icon />
                    <span>{t(($) => $.nav[child.labelKey])}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              );
            })}
          </SidebarMenu>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}

/**
 * Presentational pin row. The `label` and `iconNode` are computed by the
 * parent `PinRow` from cached issue / project detail queries — keeping
 * this component dumb means the dnd-kit / navigation wiring lives in
 * one place and the data flow is explicit.
 */
function SortablePinItem({
  pin,
  href,
  pathname,
  onUnpin,
  label,
  iconNode,
}: {
  pin: PinnedItem;
  href: string;
  pathname: string;
  onUnpin: () => void;
  label: string;
  iconNode: React.ReactNode;
}) {
  const { t } = useT("layout");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: pin.id });
  const wasDragged = useRef(false);

  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);

  const style = { transform: CSS.Transform.toString(transform), transition };
  const isActive = pathname === href;

  return (
    <SidebarMenuItem
      ref={setNodeRef}
      style={style}
      className={cn("group/pin", isDragging && "opacity-30")}
      {...attributes}
      {...listeners}
    >
      <SidebarMenuButton
        size="sm"
        isActive={isActive}
        render={<AppLink href={href} draggable={false} />}
        onClick={(event) => {
          if (wasDragged.current) {
            wasDragged.current = false;
            event.preventDefault();
            return;
          }
        }}
        className={cn(
          "text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground",
          isDragging && "pointer-events-none",
        )}
      >
        {iconNode}
        <span
          className="min-w-0 flex-1 overflow-hidden whitespace-nowrap"
          style={{
            maskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
            WebkitMaskImage: "linear-gradient(to right, black calc(100% - 12px), transparent)",
          }}
        >{label}</span>
        <Tooltip>
          <TooltipTrigger
            render={<span role="button" />}
            className="hidden size-2.5 shrink-0 items-center justify-center rounded-sm text-muted-foreground group-hover/pin:flex hover:text-foreground"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              onUnpin();
            }}
          >
            <X className="size-1" />
          </TooltipTrigger>
          <TooltipContent side="top" sideOffset={4}>{t(($) => $.sidebar.unpin_tooltip)}</TooltipContent>
        </Tooltip>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

/**
 * Smart wrapper that resolves a pin's display data (label + status/icon)
 * from the issue / project detail query cache. Both queries are declared
 * unconditionally with `enabled` gates so the hook order stays stable
 * regardless of `pin.item_type`.
 *
 * Loading: render a flat skeleton so the sidebar height doesn't jump.
 * Missing (deleted item / 404): render nothing — the row hides itself
 * until the user unpins manually or a server-side cascade catches up.
 */
function PinRow({
  pin,
  href,
  pathname,
  onUnpin,
  wsId,
}: {
  pin: PinnedItem;
  href: string;
  pathname: string;
  onUnpin: () => void;
  wsId: string;
}) {
  const isIssue = pin.item_type === "issue";
  const issueQuery = useQuery({
    ...issueDetailOptions(wsId, pin.item_id),
    enabled: isIssue,
  });
  const projectQuery = useQuery({
    ...projectDetailOptions(wsId, pin.item_id),
    enabled: !isIssue,
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
    const label = issue.title;
    const iconNode = (
      /* Override parent [&_svg]:size-4 — pinned items need smaller icons to match sm size */
      <StatusIcon status={issue.status} className="!size-3.5 shrink-0" />
    );
    return (
      <SortablePinItem
        pin={pin}
        href={href}
        pathname={pathname}
        onUnpin={onUnpin}
        label={label}
        iconNode={iconNode}
      />
    );
  }

  if (projectQuery.isPending) return <PinSkeleton />;
  if (projectQuery.isError || !projectQuery.data) return null;
  const project = projectQuery.data;
  const iconNode = <ProjectIcon project={project} size="sm" />;
  return (
    <SortablePinItem
      pin={pin}
      href={href}
      pathname={pathname}
      onUnpin={onUnpin}
      label={project.title}
      iconNode={iconNode}
    />
  );
}

function PinSkeleton() {
  return (
    <SidebarMenuItem>
      <div className="flex h-7 w-full items-center gap-2 px-2">
        <div className="size-3.5 shrink-0 rounded-sm bg-sidebar-accent/40" />
        <div className="h-3 w-24 rounded bg-sidebar-accent/40" />
      </div>
    </SidebarMenuItem>
  );
}

interface AppSidebarProps {
  /** Rendered above SidebarHeader (e.g. desktop traffic light spacer) */
  topSlot?: React.ReactNode;
  /** Rendered in the header between workspace switcher and new-issue button (e.g. search trigger) */
  searchSlot?: React.ReactNode;
  /** Extra className for SidebarHeader */
  headerClassName?: string;
  /** Extra style for SidebarHeader */
  headerStyle?: React.CSSProperties;
}

export function AppSidebar({ topSlot, searchSlot, headerClassName, headerStyle }: AppSidebarProps = {}) {
  const { t } = useT("layout");
  const { pathname, push } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const userId = useAuthStore((s) => s.user?.id);
  const logout = useLogout();
  const workspace = useCurrentWorkspace();
  const p = useWorkspacePaths();
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery(workspaceListOptions());
  const { data: myInvitations = EMPTY_INVITATIONS } = useQuery(myInvitationListOptions());
  const workspaceCreationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);

  const wsId = workspace?.id;
  const { data: inboxItems = EMPTY_INBOX } = useQuery({
    queryKey: wsId ? inboxKeys.list(wsId) : ["inbox", "disabled"],
    queryFn: () => api.listInbox(),
    enabled: !!wsId,
  });
  const unreadCount = React.useMemo(
    () => deduplicateInboxItems(inboxItems).filter((i) => !i.read).length,
    [inboxItems],
  );
  // Chat tab unread badge: IM-style total of unread *messages* across chat
  // threads (countUnreadChatMessages is the shared definition — mobile's tab
  // badge derives from the same function, keeping the platforms in agreement).
  const { data: chatSessions = [] } = useQuery({
    ...chatSessionsOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  // The session the user is reading right now must not count: the thread list
  // renders its row badge as 0 (auto mark-read is about to clear it), and a
  // reply landing in the open conversation would otherwise flash a sidebar
  // count with no matching row. "Reading right now" = a session is active, a
  // chat surface is actually showing it (chat page route or the floating
  // window), AND the app is in the foreground. When the app is backgrounded,
  // auto mark-read is suppressed (MUL-4485) so the reply stays unread — the
  // badge must count it, or the notification is silently eaten while the user
  // is away. A remembered selection while both surfaces are closed also still
  // counts, for the same reason.
  const activeChatSessionId = useChatStore((s) => s.activeSessionId);
  const floatingChatOpen = useChatStore((s) => s.isOpen);
  const appForeground = useAppForeground();
  const chatHref = p.chat();
  const viewedChatSessionId =
    appForeground && (floatingChatOpen || isNavActive(pathname, chatHref))
      ? activeChatSessionId
      : null;
  const chatUnreadCount = React.useMemo(
    () => countUnreadChatMessages(chatSessions, viewedChatSessionId),
    [chatSessions, viewedChatSessionId],
  );
  // Cross-workspace unread summary backs the workspace-switcher dot. One
  // shared cache entry across workspaces; gated on an active workspace since
  // the endpoint resolves through the workspace-member middleware.
  const { data: unreadSummary = EMPTY_INBOX_SUMMARY } = useQuery({
    ...inboxUnreadSummaryOptions(),
    enabled: !!wsId,
  });
  const otherWorkspaceUnread = React.useMemo(
    () => hasOtherWorkspaceUnread(unreadSummary, wsId),
    [unreadSummary, wsId],
  );
  // Which workspaces have unread, so the switcher dropdown can point at the
  // specific one(s) rather than just the aggregate avatar dot.
  const unreadWsIds = React.useMemo(() => unreadWorkspaceIds(unreadSummary), [unreadSummary]);
  const { data: pinnedItems = EMPTY_PINS } = useQuery({
    ...pinListOptions(wsId ?? "", userId ?? ""),
    enabled: !!wsId && !!userId,
  });
  const deletePin = useDeletePin();
  const reorderPins = useReorderPins();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));

  // Persisted collapse state per group; per-space groups manage their own
  // inside SortableSpaceGroup.
  const pinnedCollapse = useGroupCollapse("pinned");
  const workspaceCollapse = useGroupCollapse("workspace");
  const spacesCollapse = useGroupCollapse("spaces");

  // Spaces section: only spaces the user joined, in their personal order.
  const { data: mySpaces = [] } = useQuery({
    ...mySpaceListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const updateSpaceMembership = useUpdateSpaceMembership();
  // Local presentational copy for drop-animation stability — same trick as
  // the pinned rows below: follow TQ at rest, freeze during a drag so the
  // optimistic cache re-sort can't reorder the DOM while dnd-kit's drop
  // animation is still interpolating (that's what made drops snap back).
  const [localSpaces, setLocalSpaces] = useState(mySpaces);
  const [localSpacesWsId, setLocalSpacesWsId] = useState<string | null>(wsId ?? null);
  const isSpaceDraggingRef = useRef(false);
  const [spaceDragActive, setSpaceDragActive] = useState(false);
  useEffect(() => {
    if (!isSpaceDraggingRef.current) {
      setLocalSpaces(mySpaces);
    }
  }, [mySpaces]);
  useEffect(() => {
    setLocalSpacesWsId(wsId ?? null);
  }, [wsId]);
  const visibleSpaces = localSpacesWsId === (wsId ?? null) ? localSpaces : [];
  const handleSpaceDragStart = useCallback(() => {
    isSpaceDraggingRef.current = true;
    setSpaceDragActive(true);
  }, []);
  const handleSpaceDragCancel = useCallback(() => {
    isSpaceDraggingRef.current = false;
    setSpaceDragActive(false);
  }, []);
  // Fractional reorder (Linear-style): the dragged space takes the midpoint
  // of its new neighbors, so a drag is a single-row membership update. The
  // local array is rearranged first so the dropped row lands exactly where
  // the animation ends.
  const handleSpaceDragEnd = useCallback(
    (event: DragEndEvent) => {
      isSpaceDraggingRef.current = false;
      setSpaceDragActive(false);
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const oldIndex = localSpaces.findIndex((t) => t.id === active.id);
      const newIndex = localSpaces.findIndex((t) => t.id === over.id);
      if (oldIndex === -1 || newIndex === -1) return;
      const reordered = arrayMove(localSpaces, oldIndex, newIndex);
      setLocalSpaces(reordered);
      const prev = reordered[newIndex - 1];
      const next = reordered[newIndex + 1];
      const sortOrder =
        prev && next
          ? (prev.sort_order + next.sort_order) / 2
          : prev
            ? prev.sort_order + 1
            : next
              ? next.sort_order - 1
              : 1;
      updateSpaceMembership.mutate({ id: String(active.id), sort_order: sortOrder });
    },
    [localSpaces, updateSpaceMembership],
  );
  const sidebarScrollRef = useRef<HTMLDivElement>(null);
  const sidebarFadeStyle = useScrollFade(sidebarScrollRef, 24);
  const getPinHref = useCallback(
    (pin: PinnedItem) => (pin.item_type === "issue" ? p.issueDetail(pin.item_id) : p.projectDetail(pin.item_id)),
    [p],
  );

  // Local presentational copy of pinnedItems for drop-animation stability.
  // Follows TQ at rest; frozen during a drag gesture so a mid-drag cache
  // write (our own optimistic update, or a WS refetch) cannot reorder the
  // DOM under dnd-kit while its drop animation is still interpolating.
  const [localPinned, setLocalPinned] = useState<PinnedItem[]>(pinnedItems);
  const [localPinnedWsId, setLocalPinnedWsId] = useState<string | null>(wsId ?? null);
  const isDraggingRef = useRef(false);
  useEffect(() => {
    if (!isDraggingRef.current) {
      setLocalPinned(pinnedItems);
    }
  }, [pinnedItems]);
  useEffect(() => {
    setLocalPinnedWsId(wsId ?? null);
  }, [wsId]);
  const visiblePinned = localPinnedWsId === (wsId ?? null) ? localPinned : EMPTY_PINS;
  const isActivePinnedRoute = visiblePinned.some((pin) => pathname === getPinHref(pin));

  const handleDragStart = useCallback(() => {
    isDraggingRef.current = true;
  }, []);
  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      isDraggingRef.current = false;
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const oldIndex = localPinned.findIndex((p) => p.id === active.id);
      const newIndex = localPinned.findIndex((p) => p.id === over.id);
      if (oldIndex === -1 || newIndex === -1) return;
      const reordered = arrayMove(localPinned, oldIndex, newIndex);
      setLocalPinned(reordered);
      reorderPins.mutate(reordered);
    },
    [localPinned, reorderPins],
  );

  const queryClient = useQueryClient();
  const acceptInvitationMut = useMutation({
    mutationFn: (id: string) => api.acceptInvitation(id),
    // After accepting an invitation, navigate INTO the newly-joined workspace.
    // Otherwise the user stays on their current workspace and just sees the
    // new one appear in the dropdown — silent and confusing (this is MUL-820).
    onSuccess: async (_, invitationId) => {
      const invitation = myInvitations.find((i) => i.id === invitationId);
      queryClient.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
      // staleTime: 0 forces a real network fetch — we need the joined workspace
      // in the list before we can resolve its slug for navigation.
      const list = await queryClient.fetchQuery({
        ...workspaceListOptions(),
        staleTime: 0,
      });
      const joined = invitation
        ? list.find((w) => w.id === invitation.workspace_id)
        : null;
      if (joined) {
        push(paths.workspace(joined.slug).issues());
      }
    },
  });
  const declineInvitationMut = useMutation({
    mutationFn: (id: string) => api.declineInvitation(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
    },
  });

  const createIssueShortcut = useShortcut("createIssue");
  // Route-derived seed for the create-issue flow: whichever route the user is
  // standing on should default the modal, not just a page's own local "+"
  // button. Shared by the global "C" shortcut and the sidebar's "New Issue"
  // button below — neither goes through a page's `scope`-aware create
  // handler, so without this they always fell through to the personal
  // default space regardless of which space's pages the user was browsing.
  const projectRouteMatch = pathname.match(/^\/[^/]+\/projects\/([^/]+)$/);
  const spaceRouteMatch = pathname.match(/^\/[^/]+\/space\/([^/]+)/);
  const routeSpaceKey = spaceRouteMatch?.[1];
  const routeSpace = routeSpaceKey
    ? mySpaces.find((s) => s.key.toLowerCase() === routeSpaceKey.toLowerCase())
    : undefined;
  const routeCreateDefaults = projectRouteMatch
    ? { project_id: projectRouteMatch[1] }
    : routeSpace
      ? { space_id: routeSpace.id }
      : undefined;

  return (
      <Sidebar variant="inset">
        {topSlot}
        {/* Workspace Switcher */}
        <SidebarHeader className={cn("py-3", headerClassName)} style={headerStyle}>
          <SidebarMenu>
            <SidebarMenuItem>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <SidebarMenuButton>
                      <span className="relative">
                        <WorkspaceAvatar name={workspace?.name ?? "M"} avatarUrl={workspace?.avatar_url} size="sm" />
                        {/* Shared brand dot: a pending invitation OR another
                            workspace with unread inbox items. The active
                            workspace's own unread stays on the Inbox nav count
                            (below), so it is deliberately excluded here. */}
                        {(myInvitations.length > 0 || otherWorkspaceUnread) && (
                          <span className="absolute -top-0.5 -right-0.5 size-2 rounded-full bg-brand ring-1 ring-sidebar" />
                        )}
                      </span>
                      <span className="flex-1 truncate font-medium">
                        {workspace?.name ?? "Multica"}
                      </span>
                      <ChevronDown className="size-3 text-muted-foreground" />
                    </SidebarMenuButton>
                  }
                />
                <DropdownMenuContent
                  className="w-auto min-w-56"
                  align="start"
                  side="bottom"
                  sideOffset={4}
                >
                  <div className="flex items-center gap-2.5 px-2 py-1.5">
                    <ActorAvatar
                      name={user?.name ?? ""}
                      initials={(user?.name ?? "U").charAt(0).toUpperCase()}
                      avatarUrl={resolvePublicFileUrl(user?.avatar_url)}
                      size="lg"
                    />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium leading-tight">
                        {user?.name}
                      </p>
                      <p className="truncate text-xs text-muted-foreground leading-tight">
                        {user?.email}
                      </p>
                    </div>
                  </div>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuLabel className="text-xs text-muted-foreground">
                      {t(($) => $.sidebar.workspaces_label)}
                    </DropdownMenuLabel>
                    {workspaces.map((ws) => (
                      <DropdownMenuItem
                        key={ws.id}
                        render={
                          <AppLink href={paths.workspace(ws.slug).issues()} />
                        }
                      >
                        <WorkspaceAvatar name={ws.name} avatarUrl={ws.avatar_url} size="sm" />
                        <span className="flex-1 truncate">{ws.name}</span>
                        {/* Points at the specific workspace holding unread
                            inbox items. Sits in the same right-edge slot as the
                            active-workspace check; the active workspace is
                            excluded (its unread is the Inbox nav count), so dot
                            and check never collide on one row. */}
                        {ws.id !== workspace?.id && unreadWsIds.has(ws.id) && (
                          <span className="size-2 rounded-full bg-brand" />
                        )}
                        {ws.id === workspace?.id && (
                          <Check className="h-3.5 w-3.5 text-primary" />
                        )}
                      </DropdownMenuItem>
                    ))}
                    {!workspaceCreationDisabled && (
                      <DropdownMenuItem
                        onClick={() =>
                          useModalStore.getState().open("create-workspace")
                        }
                      >
                        <Plus className="h-3.5 w-3.5" />
                        {t(($) => $.sidebar.create_workspace)}
                      </DropdownMenuItem>
                    )}
                  </DropdownMenuGroup>
                  {myInvitations.length > 0 && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuGroup>
                        <DropdownMenuLabel className="text-xs text-muted-foreground">
                          {t(($) => $.sidebar.pending_invitations_label)}
                        </DropdownMenuLabel>
                        {myInvitations.map((inv) => (
                          <div key={inv.id} className="flex items-center gap-2 px-2 py-1.5">
                            <WorkspaceAvatar name={inv.workspace_name ?? "W"} size="sm" />
                            <span className="flex-1 truncate text-sm">{inv.workspace_name ?? t(($) => $.sidebar.invitation_workspace_fallback)}</span>
                            <button
                              type="button"
                              className="text-xs px-2 py-0.5 rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                              disabled={acceptInvitationMut.isPending}
                              onClick={(e) => {
                                e.stopPropagation();
                                acceptInvitationMut.mutate(inv.id);
                              }}
                            >
                              {t(($) => $.sidebar.invitation_join)}
                            </button>
                            <button
                              type="button"
                              className="text-xs px-2 py-0.5 rounded bg-muted text-muted-foreground hover:bg-muted/80 disabled:opacity-50"
                              disabled={declineInvitationMut.isPending}
                              onClick={(e) => {
                                e.stopPropagation();
                                declineInvitationMut.mutate(inv.id);
                              }}
                            >
                              {t(($) => $.sidebar.invitation_decline)}
                            </button>
                          </div>
                        ))}
                      </DropdownMenuGroup>
                    </>
                  )}
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuItem render={<AppLink href={p.settings()} />}>
                      <Settings className="h-3.5 w-3.5" />
                      {t(($) => $.nav.settings)}
                    </DropdownMenuItem>
                    <DropdownMenuItem variant="destructive" onClick={logout}>
                      <LogOut className="h-3.5 w-3.5" />
                      {t(($) => $.sidebar.log_out)}
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            </SidebarMenuItem>
          </SidebarMenu>
          <SidebarMenu>
            {searchSlot && (
              <SidebarMenuItem>
                {searchSlot}
              </SidebarMenuItem>
            )}
            <SidebarMenuItem>
              <SidebarMenuButton
                className="text-muted-foreground"
                onClick={() => openCreateIssueWithPreference(routeCreateDefaults)}
              >
                <span className="relative">
                  <SquarePen />
                  <DraftDot />
                </span>
                <span>{t(($) => $.sidebar.new_issue)}</span>
                {createIssueShortcut ? (
                  <ShortcutKeycaps shortcut={createIssueShortcut} decorative className="pointer-events-none ml-auto" />
                ) : null}
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>

        {/* Navigation */}
        <SidebarContent ref={sidebarScrollRef} style={sidebarFadeStyle}>
          <SidebarGroup className="py-1">
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {personalNav.map((item) => {
                  const href = p[item.key]();
                  const isActive = isNavActive(pathname, href);
                  return (
                    <SidebarMenuItem key={item.key}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={href} />}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{t(($) => $.nav[item.labelKey])}</span>
                        {item.key === "inbox" && unreadCount > 0 && (
                          <CappedNumberFlow
                            value={unreadCount}
                            className="ml-auto text-xs"
                          />
                        )}
                        {item.key === "chat" && chatUnreadCount > 0 && (
                          <CappedNumberFlow
                            value={chatUnreadCount}
                            className="ml-auto text-xs"
                          />
                        )}
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          {visiblePinned.length > 0 && (
            <Collapsible open={pinnedCollapse.open} onOpenChange={pinnedCollapse.onOpenChange}>
              <SidebarGroup className="group/pinned py-1">
                <SidebarGroupLabel
                  render={<CollapsibleTrigger />}
                  className="group/trigger cursor-pointer hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground"
                >
                  <span>{t(($) => $.sidebar.pinned_label)}</span>
                  <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
                  <span className="ml-auto text-[10px] text-muted-foreground opacity-0 transition-opacity group-hover/pinned:opacity-100">{visiblePinned.length}</span>
                </SidebarGroupLabel>
                <CollapsibleContent>
                  <SidebarGroupContent>
                    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
                      <SortableContext items={visiblePinned.map((p) => p.id)} strategy={verticalListSortingStrategy}>
                        <SidebarMenu className="gap-0.5">
                          {visiblePinned.map((pin: PinnedItem) => (
                            <PinRow
                              key={pin.id}
                              pin={pin}
                              href={getPinHref(pin)}
                              pathname={pathname}
                              onUnpin={() => deletePin.mutate({ itemType: pin.item_type, itemId: pin.item_id })}
                              wsId={wsId ?? ""}
                            />
                          ))}
                        </SidebarMenu>
                      </SortableContext>
                    </DndContext>
                  </SidebarGroupContent>
                </CollapsibleContent>
              </SidebarGroup>
            </Collapsible>
          )}

          {/* Workspace — shared resources. */}
          <Collapsible open={workspaceCollapse.open} onOpenChange={workspaceCollapse.onOpenChange}>
            <SidebarGroup className="group/ws py-1">
              <SidebarGroupLabel
                render={<CollapsibleTrigger />}
                className="group/trigger cursor-pointer hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground"
              >
                <span>{t(($) => $.sidebar.workspace_group)}</span>
                <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
              </SidebarGroupLabel>
              <CollapsibleContent>
                <SidebarGroupContent>
                  <SidebarMenu className="gap-0.5">
                    {[...workspaceNav, ...moreNav].map((item) => {
                      const href = p[item.key]();
                      const isActive = !isActivePinnedRoute && isNavActive(pathname, href);
                      return (
                        <SidebarMenuItem key={item.key}>
                          <SidebarMenuButton
                            isActive={isActive}
                            render={<AppLink href={href} />}
                            className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                          >
                            <item.icon />
                            <span>{t(($) => $.nav[item.labelKey])}</span>
                            {item.key === "runtimes" && hasRuntimeUpdates && (
                              <span className="ml-auto size-1.5 rounded-full bg-destructive" />
                            )}
                          </SidebarMenuButton>
                        </SidebarMenuItem>
                      );
                    })}
                  </SidebarMenu>
                </SidebarGroupContent>
              </CollapsibleContent>
            </SidebarGroup>
          </Collapsible>

          {/* Spaces — joined spaces only, in the user's personal order.
              Drag a space header to reorder; the first space doubles as the
              issue-creation default. Always rendered: a user with no joined
              spaces gets an empty-state row (join/create) instead of losing
              the section — and with it every create/browse entry point. */}
          <Collapsible open={spacesCollapse.open} onOpenChange={spacesCollapse.onOpenChange}>
              <SidebarGroup className="group/spaces relative py-1">
                {/* Rendered before the label so the label can react to it via
                    peer-hover (CSS peer only looks at *earlier* siblings).
                    Absolute positioning means this render-order swap doesn't
                    move it visually — it still paints above the label. */}
                {/* Hover-revealed, secondary until hovered itself — mirrors
                    the pinned-count affordance. Navigates to the create-space
                    page directly; sized like the space-row chevron buttons. */}
                {/* right-4 = group px-2 + label px-2, so the icon's right
                    edge sits on the same inset line as row content; top-3
                    centers the 16px button in the 32px label row (group
                    py-1). */}
                <SidebarGroupAction
                  title={t(($) => $.sidebar.new_space)}
                  className="peer/spaces-action top-3 right-4 h-4 w-4 rounded-sm text-muted-foreground opacity-0 transition-opacity hover:bg-sidebar-accent hover:text-foreground group-hover/spaces:opacity-100 [&>svg]:size-3"
                  onClick={() => push(p.spaceNew())}
                >
                  <Plus />
                </SidebarGroupAction>
                {/* The action overlaps this row's top-right corner, so its own
                    hover steals the pointer from the label underneath and the
                    label's plain hover: would drop out. peer-hover keeps the
                    row background lit while the pointer is over the action;
                    the action's own hover: still makes it visibly brighter on
                    top of that. */}
                <SidebarGroupLabel
                  render={<CollapsibleTrigger />}
                  className="group/trigger cursor-pointer hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground peer-hover/spaces-action:bg-sidebar-accent/70 peer-hover/spaces-action:text-sidebar-accent-foreground"
                >
                  <span>{t(($) => $.nav.spaces)}</span>
                  <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
                </SidebarGroupLabel>
                <CollapsibleContent>
                  {/* pt-0.5 mirrors the space rows' own children breathing
                      (py-0.5): space rows are row-styled, not plain content,
                      so they need the same gap above as below. */}
                  {/* gap-0.5 keeps a constant 2px rhythm between space
                      blocks whether they're collapsed (row→row) or expanded
                      (children→next row) — same spacing as SidebarMenu rows
                      everywhere else. */}
                  <SidebarGroupContent className="flex flex-col gap-0.5 pt-0.5">
                    {mySpaces.length === 0 && (
                      <button
                        type="button"
                        onClick={() => push(p.spaceNew())}
                        className="flex h-8 cursor-pointer items-center rounded-md px-2 text-left text-xs text-muted-foreground transition-colors hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground"
                      >
                        {t(($) => $.sidebar.spaces_empty)}
                      </button>
                    )}
                    <DndContext sensors={sensors} collisionDetection={closestCenter} measuring={spaceDragMeasuring} onDragStart={handleSpaceDragStart} onDragEnd={handleSpaceDragEnd} onDragCancel={handleSpaceDragCancel}>
                      <SortableContext items={visibleSpaces.map((space) => space.id)} strategy={verticalListSortingStrategy}>
                        {visibleSpaces.map((space) => (
                          <SortableSpaceGroup
                            key={space.id}
                            forceCollapsed={spaceDragActive}
                            space={space}
                            pathname={pathname}
                            buildHref={(pathKey, spaceKey) => p[pathKey](spaceKey)}
                            detailHref={p.spaceDetail(space.key)}
                          />
                        ))}
                      </SortableContext>
                    </DndContext>
                  </SidebarGroupContent>
                </CollapsibleContent>
              </SidebarGroup>
            </Collapsible>

        </SidebarContent>

        <SidebarFooter className="p-2">
          <JoinDiscordCard />
          <div className="flex items-center justify-end gap-1">
            {/* Settings lives as a footer icon next to Help — it's a
                low-frequency destination that doesn't earn a nav row. */}
            <AppLink
              href={p.settings()}
              aria-label={t(($) => $.nav.settings)}
              title={t(($) => $.nav.settings)}
              className={cn(
                "inline-flex size-7 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
                isNavActive(pathname, p.settings()) && "bg-accent text-foreground",
              )}
            >
              <Settings className="size-4" />
            </AppLink>
            <HelpLauncher />
          </div>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>
  );
}
