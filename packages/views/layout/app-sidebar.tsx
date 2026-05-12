"use client";

// CEREBRO-PATCH(app-sidebar-cerebro): cerebro modification of upstream file

import React, { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../navigation";
import { HelpLauncher } from "./help-launcher";
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
import {
  Inbox,
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
  X,
  Zap,
  FolderKanban,
  Hash,
  FileText,
} from "lucide-react";
import { WorkspaceAvatar } from "../workspace/workspace-avatar";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from "@multica/ui/components/ui/collapsible";
import { StatusIcon } from "../issues/components/status-icon";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useCreateModeStore } from "@multica/core/issues/stores/create-mode-store";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
// CEREBRO-PATCH(channels-flag-gate): hide channel/dm pins when cerebro_channels is OFF
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
// CEREBRO-PATCH(dashboard-nav): JEH-684 sidebar entry for cerebro dashboard
import { DashboardNavItem } from "@multica/cerebro-dashboard/views/dashboard-nav-item";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths, paths } from "@multica/core/paths";
import { workspaceListOptions, myInvitationListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { inboxKeys, deduplicateInboxItems } from "@multica/core/inbox/queries";
import { notificationsListOptions } from "@multica/cerebro-notifications/core/queries";
import { useArchiveAllNotifications } from "@multica/cerebro-notifications/core/mutations";
import { api, ApiError } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import { useMyRuntimesNeedUpdate } from "@multica/core/runtimes/hooks";
import { pinListOptions } from "@multica/core/pins/queries";
// CEREBRO-PATCH(nested-projects): sidebar renders project hierarchy from fork nesting hooks.
import { projectTreeOptions } from "@multica/core/projects/nesting";
import type { ProjectTreeItem } from "@multica/core/types";
import { useDeletePin, useReorderPins } from "@multica/core/pins/mutations";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import type { PinnedItem } from "@multica/core/types";
import { useLogout } from "../auth";
import { ProjectIcon } from "../projects/components/project-icon";
import { useT } from "../i18n";

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
const EMPTY_NOTIFICATIONS: Awaited<ReturnType<typeof api.listNotifications>> = [];
const EMPTY_PROJECT_TREE: ProjectTreeItem[] = [];

// Nav items reference WorkspacePaths method names so they can be resolved
// against the current workspace slug at render time (see AppSidebar body).
// Only parameterless paths are valid nav destinations.
type NavKey =
  | "inbox"
  | "myIssues"
  | "issues"
  | "projects"
  | "documents"
  | "autopilots"
  | "agents"
  | "runtimes"
  | "skills"
  | "settings";

// Static schema (key + icon) — labels resolved at render via useT("layout").
type NavLabelKey =
  | "inbox"
  | "my_issues"
  | "issues"
  | "projects"
  | "documents"
  | "autopilots"
  | "agents"
  | "runtimes"
  | "skills"
  | "settings";

const personalNav: { key: NavKey; labelKey: NavLabelKey; icon: typeof Inbox }[] = [
  { key: "inbox", labelKey: "inbox", icon: Inbox },
  { key: "myIssues", labelKey: "my_issues", icon: CircleUser },
];

const workspaceNav: { key: NavKey; labelKey: NavLabelKey; icon: typeof Inbox }[] = [
  { key: "issues", labelKey: "issues", icon: ListTodo },
  // CEREBRO-PATCH(no-duplicate-projects-nav): JEH-1004 — Projects entry moved to the nested-projects Collapsible block below.
  // CEREBRO-PATCH(sidebar-documents-nav): workspace documents page
  { key: "documents", labelKey: "documents", icon: FileText },
  { key: "autopilots", labelKey: "autopilots", icon: Zap },
  { key: "agents", labelKey: "agents", icon: Bot },
];

const configureNav: { key: NavKey; labelKey: NavLabelKey; icon: typeof Inbox }[] = [
  { key: "runtimes", labelKey: "runtimes", icon: Monitor },
  { key: "skills", labelKey: "skills", icon: BookOpenText },
  { key: "settings", labelKey: "settings", icon: Settings },
];

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
}

// Channels and DMs live in the issue table (kind in 'channel' | 'dm') so until
// they get dedicated pages, route them through issueDetail.
// CEREBRO-PATCH(sidebar-channels-pins): channel/dm pin types route via issueDetail
function pinHref(p: ReturnType<typeof useWorkspacePaths>, pin: PinnedItem): string {
  switch (pin.item_type) {
    case "project":
      return p.projectDetail(pin.item_id);
    case "issue":
    case "channel":
    case "dm":
      return p.issueDetail(pin.item_id);
  }
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
  onNavigate,
  label,
  iconNode,
}: {
  pin: PinnedItem;
  href: string;
  pathname: string;
  onUnpin: () => void;
  onNavigate: () => void;
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
          onNavigate();
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
  onNavigate,
  wsId,
}: {
  pin: PinnedItem;
  href: string;
  pathname: string;
  onUnpin: () => void;
  onNavigate: () => void;
  wsId: string;
}) {
  // CEREBRO-PATCH(sidebar-channels-pins): channel/dm pins resolve via issue table
  const usesIssueQuery =
    pin.item_type === "issue" || pin.item_type === "channel" || pin.item_type === "dm";
  const issueQuery = useQuery({
    ...issueDetailOptions(wsId, pin.item_id),
    enabled: usesIssueQuery,
  });
  const projectQuery = useQuery({
    ...projectDetailOptions(wsId, pin.item_id),
    enabled: !usesIssueQuery,
  });

  const triggeredRef = useRef(false);
  useEffect(() => {
    const err = usesIssueQuery ? issueQuery.error : projectQuery.error;
    if (err instanceof ApiError && err.status === 404 && !triggeredRef.current) {
      triggeredRef.current = true;
      onUnpin();
    }
  }, [usesIssueQuery, issueQuery.error, onUnpin, projectQuery.error]);

  if (usesIssueQuery) {
    if (issueQuery.isPending) return <PinSkeleton />;
    if (issueQuery.isError || !issueQuery.data) return null;
    const issue = issueQuery.data;
    let label: string;
    let iconNode: React.ReactNode;
    if (pin.item_type === "channel") {
      label = `#${issue.title}`;
      iconNode = <Hash className="!size-3.5 shrink-0" />;
    } else if (pin.item_type === "dm") {
      label = issue.title;
      iconNode = (
        <ActorAvatar name={issue.title} initials={(issue.title || "?").charAt(0).toUpperCase()} size={14} />
      );
    } else {
      label = issue.identifier ? `${issue.identifier} ${issue.title}` : issue.title;
      iconNode = (
        /* Override parent [&_svg]:size-4 — pinned items need smaller icons to match sm size */
        <StatusIcon status={issue.status} className="!size-3.5 shrink-0" />
      );
    }
    return (
      <SortablePinItem
        pin={pin}
        href={href}
        pathname={pathname}
        onUnpin={onUnpin}
        onNavigate={onNavigate}
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
      onNavigate={onNavigate}
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

function filterActiveProjectTree(projects: ProjectTreeItem[]): ProjectTreeItem[] {
  return projects
    .filter((p) => p.status !== "completed" && p.status !== "cancelled")
    .map((p) => ({ ...p, children: filterActiveProjectTree(p.children ?? []) }));
}

function projectTreeContainsPath(project: ProjectTreeItem, pathname: string, projectHref: (id: string) => string): boolean {
  if (pathname === projectHref(project.id)) return true;
  return (project.children ?? []).some((child) => projectTreeContainsPath(child, pathname, projectHref));
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
  const { isMobile, setOpenMobile } = useSidebar();
  // Auto-close the mobile sheet whenever the route changes — desktop sidebar is unaffected.
  const lastPathRef = useRef(pathname);
  useEffect(() => {
    if (lastPathRef.current !== pathname) {
      lastPathRef.current = pathname;
      if (isMobile) setOpenMobile(false);
    }
  }, [pathname, isMobile, setOpenMobile]);
  // Pathname-based close above misses two cases: clicking a nav link while
  // already on that pathname, and route changes that only update query params
  // (Next.js usePathname strips the search string). Wire this onto every
  // nav-link click so mobile always closes regardless.
  const handleNavClick = useCallback(() => {
    if (isMobile) setOpenMobile(false);
  }, [isMobile, setOpenMobile]);
  const user = useAuthStore((s) => s.user);
  const userId = useAuthStore((s) => s.user?.id);
  const logout = useLogout();
  const workspace = useCurrentWorkspace();
  const p = useWorkspacePaths();
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery(workspaceListOptions());
  const { data: myInvitations = EMPTY_INVITATIONS } = useQuery(myInvitationListOptions());

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
  const { data: notifications = EMPTY_NOTIFICATIONS } = useQuery({
    ...notificationsListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const visibleNotifications = React.useMemo(
    () => notifications.filter((n) => !n.archived),
    [notifications],
  );
  const archiveAllNotifications = useArchiveAllNotifications();
  const hasRuntimeUpdates = useMyRuntimesNeedUpdate(wsId);
  const { data: projectTree = EMPTY_PROJECT_TREE } = useQuery({
    ...projectTreeOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const activeProjects = React.useMemo(
    () => filterActiveProjectTree(projectTree),
    [projectTree],
  );
  const expandedProjectPrefs = (user?.preferences?.project_sidebar_expanded ?? {}) as Record<string, boolean>;
  const [localExpandedProjects, setLocalExpandedProjects] = useState<Record<string, boolean>>(expandedProjectPrefs);
  useEffect(() => {
    setLocalExpandedProjects(expandedProjectPrefs);
  }, [user?.preferences?.project_sidebar_expanded]);
  const setProjectExpanded = useCallback((projectId: string, expanded: boolean) => {
    const next = { ...localExpandedProjects, [projectId]: expanded };
    setLocalExpandedProjects(next);
    api.updateMyPreferences({ project_sidebar_expanded: next })
      .then((updatedUser) => useAuthStore.getState().setUser(updatedUser))
      .catch(() => setLocalExpandedProjects(localExpandedProjects));
  }, [localExpandedProjects]);
  const { data: pinnedItemsRaw = EMPTY_PINS } = useQuery({
    ...pinListOptions(wsId ?? "", userId ?? ""),
    enabled: !!wsId && !!userId,
  });
  // CEREBRO-PATCH(channels-flag-gate): filter channel/dm pins when feature is disabled
  const channelsEnabled = useFeatureFlag("cerebro_channels");
  const pinnedItems = React.useMemo(
    () =>
      channelsEnabled
        ? pinnedItemsRaw
        : pinnedItemsRaw.filter((p: PinnedItem) => p.item_type !== "channel" && p.item_type !== "dm"),
    [pinnedItemsRaw, channelsEnabled],
  );
  const deletePin = useDeletePin();
  const reorderPins = useReorderPins();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));

  // Local presentational copy of pinnedItems for drop-animation stability.
  // Follows TQ at rest; frozen during a drag gesture so a mid-drag cache
  // write (our own optimistic update, or a WS refetch) cannot reorder the
  // DOM under dnd-kit while its drop animation is still interpolating.
  const [localPinned, setLocalPinned] = useState<PinnedItem[]>(pinnedItems);
  const isDraggingRef = useRef(false);
  useEffect(() => {
    if (!isDraggingRef.current) {
      setLocalPinned(pinnedItems);
    }
  }, [pinnedItems]);

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

  // Global "C" shortcut: opens whichever create mode the user landed on last
  // (agent vs manual), persisted in useCreateModeStore. The mode switch lives
  // inside both modal footers so users can flip without remembering which
  // shortcut goes where — `c` always means "open the create flow I prefer".
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "c" && e.key !== "C") return;
      if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      const tag = (e.target as HTMLElement)?.tagName;
      const isEditable =
        tag === "INPUT" ||
        tag === "TEXTAREA" ||
        tag === "SELECT" ||
        (e.target as HTMLElement)?.isContentEditable;
      if (isEditable) return;
      if (useModalStore.getState().modal) return;
      e.preventDefault();
      const lastMode = useCreateModeStore.getState().lastMode;
      if (lastMode === "manual") {
        // Auto-fill project when on a project detail page (manual form only —
        // agent mode lets the agent infer project from the prompt).
        const projectMatch = pathname.match(/^\/[^/]+\/projects\/([^/]+)$/);
        const data = projectMatch ? { project_id: projectMatch[1] } : undefined;
        useModalStore.getState().open("create-issue", data);
      } else {
        useModalStore.getState().open("quick-create-issue");
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [pathname]);

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
                        <WorkspaceAvatar name={workspace?.name ?? "M"} size="sm" />
                        {myInvitations.length > 0 && (
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
                      avatarUrl={user?.avatar_url}
                      size={32}
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
                        <WorkspaceAvatar name={ws.name} size="sm" />
                        <span className="flex-1 truncate">{ws.name}</span>
                        {ws.id === workspace?.id && (
                          <Check className="h-3.5 w-3.5 text-primary" />
                        )}
                      </DropdownMenuItem>
                    ))}
                    <DropdownMenuItem
                      onClick={() =>
                        useModalStore.getState().open("create-workspace")
                      }
                    >
                      <Plus className="h-3.5 w-3.5" />
                      {t(($) => $.sidebar.create_workspace)}
                    </DropdownMenuItem>
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
                onClick={() => useModalStore.getState().open("quick-create-issue")}
              >
                <span className="relative">
                  <SquarePen />
                  <DraftDot />
                </span>
                <span>{t(($) => $.sidebar.new_issue)}</span>
                <kbd className="pointer-events-none ml-auto inline-flex h-5 select-none items-center gap-0.5 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground">{t(($) => $.sidebar.new_issue_shortcut)}</kbd>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>

        {/* Navigation */}
        <SidebarContent>
          <SidebarGroup>
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
                        onClick={handleNavClick}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{t(($) => $.nav[item.labelKey])}</span>
                        {item.key === "inbox" && unreadCount > 0 && (
                          <span className="ml-auto text-xs">
                            {unreadCount > 99 ? "99+" : unreadCount}
                          </span>
                        )}
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          {localPinned.length > 0 && (
            <Collapsible defaultOpen>
              <SidebarGroup className="group/pinned">
                <SidebarGroupLabel
                  render={<CollapsibleTrigger />}
                  className="group/trigger cursor-pointer hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground"
                >
                  <span>{t(($) => $.sidebar.pinned_label)}</span>
                  <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
                  <span className="ml-auto text-[10px] text-muted-foreground opacity-0 transition-opacity group-hover/pinned:opacity-100">{localPinned.length}</span>
                </SidebarGroupLabel>
                <CollapsibleContent>
                  <SidebarGroupContent>
                    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
                      <SortableContext items={localPinned.map((p) => p.id)} strategy={verticalListSortingStrategy}>
                        <SidebarMenu className="gap-0.5">
                          {localPinned.map((pin: PinnedItem) => (
                            <PinRow
                              key={pin.id}
                              pin={pin}
                              href={pinHref(p, pin)}
                              pathname={pathname}
                              onUnpin={() => deletePin.mutate({ itemType: pin.item_type, itemId: pin.item_id })}
                              onNavigate={handleNavClick}
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

          <SidebarGroup>
            <SidebarGroupLabel>{t(($) => $.sidebar.workspace_group)}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {/* CEREBRO-PATCH(dashboard-nav): JEH-684 cerebro dashboard entry in workspace group */}
                <DashboardNavItem workspaceSlug={workspace?.slug ?? ""} onClick={handleNavClick} />
                {workspaceNav.map((item) => {
                  const href = p[item.key]();
                  const isActive = isNavActive(pathname, href);
                  return (
                    <SidebarMenuItem key={item.key}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={href} />}
                        onClick={handleNavClick}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{t(($) => $.nav[item.labelKey])}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
                {/* Projects — collapsible nav item with sub-items */}
                <Collapsible defaultOpen>
                  <SidebarMenuItem>
                    <CollapsibleTrigger
                      className={cn(
                        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors cursor-pointer",
                        "text-muted-foreground hover:bg-sidebar-accent/70",
                        pathname.includes("/projects") && "bg-sidebar-accent text-sidebar-accent-foreground",
                      )}
                    >
                      <FolderKanban className="size-4 shrink-0" />
                      <AppLink href={p.projects()} onClick={(e: React.MouseEvent) => { e.stopPropagation(); handleNavClick(); }} className="flex-1 text-left hover:underline">
                        Projects
                      </AppLink>
                      <ChevronRight className="size-3 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[panel-open]:rotate-90" />
                    </CollapsibleTrigger>
                    <CollapsibleContent>
                      <SidebarMenu className="gap-0 mt-0.5">
                        <SidebarMenuItem>
                          <SidebarMenuButton
                            className="text-muted-foreground hover:bg-sidebar-accent/70 h-7"
                            onClick={() => useModalStore.getState().open("create-project")}
                          >
                            <Plus className="size-3.5" />
                            <span>New Project</span>
                          </SidebarMenuButton>
                        </SidebarMenuItem>
                        {activeProjects.map((project) => {
                          const renderProject = (item: ProjectTreeItem, level: number): React.ReactNode => {
                            const href = p.projectDetail(item.id);
                            const isActive = pathname === href;
                            const children = item.children ?? [];
                            const hasChildren = children.length > 0;
                            const isExpanded = localExpandedProjects[item.id] ?? true;
                            const hasActiveChild = children.some((child) => projectTreeContainsPath(child, pathname, p.projectDetail));
                            // CEREBRO-PATCH(nested-projects-indent): each nesting level adds 12px margin + 8px padding (v3 mockup).
                            const nestedStyle = level > 0 ? { marginLeft: level * 12, paddingLeft: level * 8 } : undefined;
                            return (
                              <React.Fragment key={item.id}>
                                <SidebarMenuItem>
                                  <div
                                    className={cn(
                                      "relative",
                                      level > 0 && "border-l border-sidebar-border/70",
                                    )}
                                    style={nestedStyle}
                                  >
                                    <SidebarMenuButton
                                      isActive={isActive}
                                      render={<AppLink href={href} />}
                                      onClick={handleNavClick}
                                      className={cn(
                                        "h-7 text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground",
                                        hasActiveChild && "bg-sidebar-accent/45 text-sidebar-accent-foreground",
                                      )}
                                    >
                                      {hasChildren ? (
                                        <span
                                          role="button"
                                          tabIndex={0}
                                          className="flex size-3.5 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:text-foreground"
                                          onClick={(event) => {
                                            event.preventDefault();
                                            event.stopPropagation();
                                            setProjectExpanded(item.id, !isExpanded);
                                          }}
                                          onKeyDown={(event) => {
                                            if (event.key !== "Enter" && event.key !== " ") return;
                                            event.preventDefault();
                                            event.stopPropagation();
                                            setProjectExpanded(item.id, !isExpanded);
                                          }}
                                        >
                                          <ChevronRight className={cn("!size-3 transition-transform", isExpanded && "rotate-90")} />
                                        </span>
                                      ) : (
                                        <span className="size-3.5 shrink-0" />
                                      )}
                                      {level === 0 && <ProjectIcon project={item} size="sm" />}
                                      <span className="truncate">{item.title}</span>
                                      {level === 0 && item.issue_count > 0 && (
                                        <span className="ml-auto text-[10px] text-muted-foreground">
                                          {item.done_count}/{item.issue_count}
                                        </span>
                                      )}
                                    </SidebarMenuButton>
                                  </div>
                                </SidebarMenuItem>
                                {hasChildren && isExpanded && children.map((child) => renderProject(child, level + 1))}
                              </React.Fragment>
                            );
                          };
                          return renderProject(project, 0);
                        })}
                      </SidebarMenu>
                    </CollapsibleContent>
                  </SidebarMenuItem>
                </Collapsible>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <SidebarGroup>
            <SidebarGroupLabel>{t(($) => $.sidebar.configure_group)}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {configureNav.map((item) => {
                  const href = p[item.key]();
                  const isActive = isNavActive(pathname, href);
                  return (
                    <SidebarMenuItem key={item.key}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={href} />}
                        onClick={handleNavClick}
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
          </SidebarGroup>
        </SidebarContent>

        <SidebarFooter className="p-2">
          {/* CEREBRO-PATCH(sidebar-notifications-footer): notification link + user popover */}
          {wsId && (
            <AppLink
              href={p.notifications()}
              data-testid="sidebar-notifications-link"
              onClick={handleNavClick}
              className={cn(
                "group/notif flex items-center justify-between gap-2 rounded-md px-3 py-2 text-xs font-semibold uppercase tracking-wide transition-colors",
                pathname === p.notifications()
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-foreground",
              )}
            >
              <span className="flex items-center gap-2">
                Notifications
                {visibleNotifications.length > 0 && (
                  <span
                    data-testid="sidebar-notifications-count"
                    className="rounded-full bg-muted px-1.5 text-[10px] font-semibold normal-case tracking-normal text-muted-foreground"
                  >
                    {visibleNotifications.length}
                  </span>
                )}
              </span>
              {visibleNotifications.length > 0 && (
                <button
                  type="button"
                  data-testid="sidebar-notifications-clear"
                  className="text-[11px] font-medium normal-case tracking-normal text-muted-foreground opacity-0 transition-opacity hover:text-foreground group-hover/notif:opacity-100"
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    archiveAllNotifications.mutate();
                  }}
                >
                  Clear
                </button>
              )}
            </AppLink>
          )}
          <div className="flex items-center gap-2 border-t pt-2">
            <Popover>
              <PopoverTrigger className="flex flex-1 min-w-0 items-center gap-2.5 rounded-md px-2 py-1.5 hover:bg-accent transition-colors cursor-pointer">
                <ActorAvatar
                  name={user?.name ?? ""}
                  initials={(user?.name ?? "U").charAt(0).toUpperCase()}
                  avatarUrl={user?.avatar_url}
                  size={28}
                />
                <div className="min-w-0 flex-1 text-left">
                  <p className="truncate text-sm font-medium leading-tight">
                    {user?.name}
                  </p>
                  <p className="truncate text-xs text-muted-foreground leading-tight">
                    {user?.email}
                  </p>
                </div>
              </PopoverTrigger>
              <PopoverContent side="top" sideOffset={8} align="start" className="w-48 p-0">
                <div className="flex items-center gap-2.5 px-2.5 py-2 border-b">
                  <ActorAvatar
                    name={user?.name ?? ""}
                    initials={(user?.name ?? "U").charAt(0).toUpperCase()}
                    avatarUrl={user?.avatar_url}
                    size={32}
                  />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">
                      {user?.name}
                    </p>
                    <p className="truncate text-xs text-muted-foreground">
                      {user?.email}
                    </p>
                  </div>
                </div>
                <div className="p-1">
                  <button
                    onClick={logout}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-destructive hover:bg-destructive/10 transition-colors cursor-pointer"
                  >
                    <LogOut className="h-3.5 w-3.5" />
                    Log out
                  </button>
                </div>
              </PopoverContent>
            </Popover>
            <HelpLauncher />
          </div>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>
  );
}
