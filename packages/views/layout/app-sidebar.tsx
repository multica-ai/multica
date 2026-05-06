"use client";

// CEREBRO-PATCH(app-sidebar-cerebro): cerebro modification of upstream file

import React, { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../navigation";
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
import type { IssueStatus } from "@multica/core/types";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarFooter,
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
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths, paths } from "@multica/core/paths";
import { workspaceListOptions, myInvitationListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { inboxKeys, deduplicateInboxItems } from "@multica/core/inbox/queries";
import { notificationsListOptions } from "@multica/cerebro-notifications/core/queries";
import { useArchiveAllNotifications } from "@multica/cerebro-notifications/core/mutations";
import { api } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import { useMyRuntimesNeedUpdate } from "@multica/core/runtimes/hooks";
import { pinListOptions } from "@multica/core/pins/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { getProjectColor } from "@multica/core/projects/config";
import type { Project } from "@multica/core/types";
import { useDeletePin, useReorderPins } from "@multica/core/pins/mutations";
import type { PinnedItem } from "@multica/core/types";
import { useLogout } from "../auth";

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
const EMPTY_PROJECTS: Project[] = [];

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

const personalNav: { key: NavKey; label: string; icon: typeof Inbox }[] = [
  { key: "inbox", label: "Inbox", icon: Inbox },
  { key: "myIssues", label: "My Issues", icon: CircleUser },
];

const workspaceNav: { key: NavKey; label: string; icon: typeof Inbox }[] = [
  { key: "issues", label: "Issues", icon: ListTodo },
  { key: "documents", label: "Documents", icon: FileText },
  { key: "autopilots", label: "Autopilot", icon: Zap },
  { key: "agents", label: "Agents", icon: Bot },
];

const configureNav: { key: NavKey; label: string; icon: typeof Inbox }[] = [
  { key: "runtimes", label: "Runtimes", icon: Monitor },
  { key: "skills", label: "Skills", icon: BookOpenText },
  { key: "settings", label: "Settings", icon: Settings },
];

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
}

// Channels and DMs live in the issue table (kind in 'channel' | 'dm') so until
// they get dedicated pages, route them through issueDetail.
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

function SortablePinItem({ pin, href, pathname, onUnpin, onNavigate }: { pin: PinnedItem; href: string; pathname: string; onUnpin: () => void; onNavigate: () => void }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: pin.id });
  const wasDragged = useRef(false);

  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);

  const style = { transform: CSS.Transform.toString(transform), transition };
  const isActive = pathname === href;
  const label =
    pin.item_type === "issue" && pin.identifier
      ? `${pin.identifier} ${pin.title}`
      : pin.item_type === "channel"
        ? `#${pin.title}`
        : pin.title;

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
        {pin.item_type === "issue" && pin.status ? (
          /* Override parent [&_svg]:size-4 — pinned items need smaller icons to match sm size */
          <StatusIcon status={pin.status as IssueStatus} className="!size-3.5 shrink-0" />
        ) : pin.item_type === "channel" ? (
          <Hash className="!size-3.5 shrink-0" />
        ) : pin.item_type === "dm" ? (
          <ActorAvatar name={pin.title} initials={(pin.title || "?").charAt(0).toUpperCase()} size={14} />
        ) : (
          <span className="flex size-3.5 shrink-0 items-center justify-center text-xs leading-none">{pin.icon || "📁"}</span>
        )}
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
          <TooltipContent side="top" sideOffset={4}>Unpin</TooltipContent>
        </Tooltip>
      </SidebarMenuButton>
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
  const { data: projects = EMPTY_PROJECTS } = useQuery({
    ...projectListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const activeProjects = React.useMemo(
    () => projects.filter((p: Project) => p.status !== "completed" && p.status !== "cancelled"),
    [projects],
  );
  const { data: pinnedItems = EMPTY_PINS } = useQuery({
    ...pinListOptions(wsId ?? "", userId ?? ""),
    enabled: !!wsId && !!userId,
  });
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

  // Auto-fill project when on a project detail page
  const projectMatch = pathname.match(/^\/[^/]+\/projects\/([^/]+)$/);
  const createIssueData = projectMatch ? { project_id: projectMatch[1] } : undefined;

  // Global "C" shortcut to open create-issue modal (like Linear)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "c" && !e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey) {
        const tag = (e.target as HTMLElement)?.tagName;
        const isEditable =
          tag === "INPUT" ||
          tag === "TEXTAREA" ||
          tag === "SELECT" ||
          (e.target as HTMLElement)?.isContentEditable;
        if (isEditable) return;
        if (useModalStore.getState().modal) return;
        e.preventDefault();
        useModalStore.getState().open("create-issue", createIssueData);
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
                      <WorkspaceAvatar name={workspace?.name ?? "M"} size="sm" />
                      <span className="flex-1 truncate font-medium">
                        {workspace?.name ?? "Multica"}
                      </span>
                      <ChevronDown className="size-3 text-muted-foreground" />
                    </SidebarMenuButton>
                  }
                />
                <DropdownMenuContent
                  className="w-auto"
                  align="start"
                  side="bottom"
                  sideOffset={4}
                >
                  <DropdownMenuGroup>
                    <DropdownMenuLabel className="text-xs text-muted-foreground">
                      {user?.email}
                    </DropdownMenuLabel>
                  </DropdownMenuGroup>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuLabel className="text-xs text-muted-foreground">
                      Workspaces
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
                      Create workspace
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                  {myInvitations.length > 0 && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuGroup>
                        <DropdownMenuLabel className="text-xs text-muted-foreground">
                          Pending invitations
                        </DropdownMenuLabel>
                        {myInvitations.map((inv) => (
                          <div key={inv.id} className="flex items-center gap-2 px-2 py-1.5">
                            <WorkspaceAvatar name={inv.workspace_name ?? "W"} size="sm" />
                            <span className="flex-1 truncate text-sm">{inv.workspace_name ?? "Workspace"}</span>
                            <button
                              type="button"
                              className="text-xs px-2 py-0.5 rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                              disabled={acceptInvitationMut.isPending}
                              onClick={(e) => {
                                e.stopPropagation();
                                acceptInvitationMut.mutate(inv.id);
                              }}
                            >
                              Join
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
                              Decline
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
                      Log out
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
                onClick={() => useModalStore.getState().open("create-issue", createIssueData)}
              >
                <span className="relative">
                  <SquarePen />
                  <DraftDot />
                </span>
                <span>New Issue</span>
                <kbd className="pointer-events-none ml-auto inline-flex h-5 select-none items-center gap-0.5 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground">C</kbd>
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
                  const isActive = pathname === href;
                  return (
                    <SidebarMenuItem key={item.key}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={href} />}
                        onClick={handleNavClick}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{item.label}</span>
                        {item.label === "Inbox" && unreadCount > 0 && (
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
                  <span>Pinned</span>
                  <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
                  <span className="ml-auto text-[10px] text-muted-foreground opacity-0 transition-opacity group-hover/pinned:opacity-100">{localPinned.length}</span>
                </SidebarGroupLabel>
                <CollapsibleContent>
                  <SidebarGroupContent>
                    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
                      <SortableContext items={localPinned.map((p) => p.id)} strategy={verticalListSortingStrategy}>
                        <SidebarMenu className="gap-0.5">
                          {localPinned.map((pin: PinnedItem) => (
                            <SortablePinItem
                              key={pin.id}
                              pin={pin}
                              href={pinHref(p, pin)}
                              pathname={pathname}
                              onUnpin={() => deletePin.mutate({ itemType: pin.item_type, itemId: pin.item_id })}
                              onNavigate={handleNavClick}
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
            <SidebarGroupLabel>Workspace</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {workspaceNav.map((item) => {
                  const href = p[item.key]();
                  const isActive = pathname === href;
                  return (
                    <SidebarMenuItem key={item.key}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={href} />}
                        onClick={handleNavClick}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{item.label}</span>
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
                            className="text-muted-foreground hover:bg-sidebar-accent/70 pl-8 h-7"
                            onClick={() => useModalStore.getState().open("create-project")}
                          >
                            <Plus className="size-3.5" />
                            <span>New Project</span>
                          </SidebarMenuButton>
                        </SidebarMenuItem>
                        {activeProjects.map((project: Project) => {
                          const href = p.projectDetail(project.id);
                          const isActive = pathname === href;
                          return (
                            <SidebarMenuItem key={project.id}>
                              <SidebarMenuButton
                                isActive={isActive}
                                render={<AppLink href={href} />}
                                onClick={handleNavClick}
                                className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground pl-8 h-7"
                              >
                                {project.icon ? (
                                  <span className="text-sm shrink-0">{project.icon}</span>
                                ) : (
                                  <span className={cn("size-2 rounded-full shrink-0", getProjectColor(project.color).dot)} />
                                )}
                                <span className="truncate">{project.title}</span>
                                {project.issue_count > 0 && (
                                  <span className="ml-auto text-[10px] text-muted-foreground">
                                    {project.done_count}/{project.issue_count}
                                  </span>
                                )}
                              </SidebarMenuButton>
                            </SidebarMenuItem>
                          );
                        })}
                      </SidebarMenu>
                    </CollapsibleContent>
                  </SidebarMenuItem>
                </Collapsible>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <SidebarGroup>
            <SidebarGroupLabel>Configure</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {configureNav.map((item) => {
                  const href = p[item.key]();
                  const isActive = pathname === href;
                  return (
                    <SidebarMenuItem key={item.key}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={href} />}
                        onClick={handleNavClick}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{item.label}</span>
                        {item.label === "Runtimes" && hasRuntimeUpdates && (
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
          <div className="border-t pt-2">
            <Popover>
              <PopoverTrigger className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 hover:bg-accent transition-colors cursor-pointer">
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
          </div>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>
  );
}
