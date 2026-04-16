"use client";

import React, { useCallback, useEffect, useRef } from "react";
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
  FolderKanban,
  X,
  Zap,
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
import { api } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import { useMyRuntimesNeedUpdate } from "@multica/core/runtimes/hooks";
import { pinListOptions } from "@multica/core/pins/queries";
import { useDeletePin, useReorderPins } from "@multica/core/pins/mutations";
import { projectListOptions } from "@multica/core/projects/queries";
import { useCreateIssue } from "@multica/core/issues/mutations";
import type { PinnedItem, Project, WorkspaceRepo } from "@multica/core/types";
import { useLogout } from "../auth";

function repoShortName(repo: WorkspaceRepo): string {
  if (repo.local_path) {
    const parts = repo.local_path.split("/").filter(Boolean);
    const last = parts[parts.length - 1];
    if (last) return last;
  }
  const trimmed = (repo.url ?? "").trim().replace(/\.git$/i, "");
  const parts = trimmed.split("/").filter(Boolean);
  return parts[parts.length - 1] || repo.url || "repo";
}

function repoDescriptionTemplate(repo: WorkspaceRepo): string {
  const name = repoShortName(repo);
  const lines: string[] = [];
  if (repo.local_path) lines.push(`Local path: \`${repo.local_path}\``);
  if (repo.url) lines.push(`Git: ${repo.url}`);
  return `Repository: **${name}**\n${lines.join("\n")}\n\n`;
}

// Nav items reference WorkspacePaths method names so they can be resolved
// against the current workspace slug at render time (see AppSidebar body).
// Only parameterless paths are valid nav destinations.
type NavKey =
  | "inbox"
  | "myIssues"
  | "issues"
  | "projects"
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
  { key: "projects", label: "Projects", icon: FolderKanban },
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

function SortablePinItem({ pin, href, pathname, onUnpin }: { pin: PinnedItem; href: string; pathname: string; onUnpin: () => void }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: pin.id });
  const wasDragged = useRef(false);

  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);

  const style = { transform: CSS.Transform.toString(transform), transition };
  const isActive = pathname === href;
  const label = pin.item_type === "issue" && pin.identifier ? `${pin.identifier} ${pin.title}` : pin.title;

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
        render={<AppLink href={href} />}
        onClick={(event) => {
          if (wasDragged.current) {
            wasDragged.current = false;
            event.preventDefault();
            return;
          }
        }}
        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
      >
        {pin.item_type === "issue" && pin.status ? (
          /* Override parent [&_svg]:size-4 — pinned items need smaller icons to match sm size */
          <StatusIcon status={pin.status as IssueStatus} className="!size-3.5 shrink-0" />
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

function ProjectSidebarRow({
  project,
  href,
  isActive,
  repos,
}: {
  project: Project;
  href: string;
  isActive: boolean;
  repos: WorkspaceRepo[];
}) {
  const createIssue = useCreateIssue();

  const openProjectModal = () => {
    useModalStore.getState().open("create-issue", { project_id: project.id });
  };

  const openRepoModal = (repo: WorkspaceRepo) => {
    useModalStore.getState().open("create-issue", {
      project_id: project.id,
      description: repoDescriptionTemplate(repo),
    });
  };

  const createRepoIssueQuick = (repo: WorkspaceRepo, title: string) => {
    createIssue.mutate({
      title,
      project_id: project.id,
      description: repoDescriptionTemplate(repo),
    });
  };

  return (
    <Collapsible defaultOpen>
      <SidebarMenuItem className="group/proj">
        <SidebarMenuButton
          size="sm"
          isActive={isActive}
          render={<AppLink href={href} />}
          className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
        >
          <CollapsibleTrigger
            className="flex size-3.5 shrink-0 items-center justify-center rounded-sm hover:bg-sidebar-accent"
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
            }}
          >
            <ChevronRight className="!size-3 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/proj:rotate-90" />
          </CollapsibleTrigger>
          {project.icon ? (
            <span className="flex size-3.5 shrink-0 items-center justify-center text-xs leading-none">{project.icon}</span>
          ) : (
            <FolderKanban className="!size-3.5 shrink-0" />
          )}
          <span className="min-w-0 flex-1 truncate">{project.title}</span>
          <Tooltip>
            <TooltipTrigger
              render={<span role="button" />}
              className="hidden size-4 shrink-0 items-center justify-center rounded-sm text-muted-foreground group-hover/proj:flex hover:text-foreground hover:bg-sidebar-accent"
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                openProjectModal();
              }}
            >
              <Plus className="size-3" />
            </TooltipTrigger>
            <TooltipContent side="top" sideOffset={4}>New issue in {project.title}</TooltipContent>
          </Tooltip>
        </SidebarMenuButton>
      </SidebarMenuItem>
      <CollapsibleContent>
        <SidebarMenu className="gap-0.5 ml-5 border-l border-sidebar-border pl-1 mt-0.5">
          {repos.length === 0 ? (
            <li className="px-2 py-1 text-[10px] text-muted-foreground italic">No repos — assign in Settings</li>
          ) : (
            repos.map((repo) => (
              <RepoSidebarRow
                key={repo.url || repo.local_path || `repo-${project.id}`}
                repo={repo}
                onOpenModal={() => openRepoModal(repo)}
                onQuickCreate={(title) => createRepoIssueQuick(repo, title)}
                isSubmitting={createIssue.isPending}
              />
            ))
          )}
        </SidebarMenu>
      </CollapsibleContent>
    </Collapsible>
  );
}

function RepoSidebarRow({
  repo,
  onOpenModal,
  onQuickCreate,
  isSubmitting,
}: {
  repo: WorkspaceRepo;
  onOpenModal: () => void;
  onQuickCreate: (title: string) => void;
  isSubmitting: boolean;
}) {
  const [quickTitle, setQuickTitle] = React.useState("");
  const name = repoShortName(repo);

  const submit = () => {
    const t = quickTitle.trim();
    if (!t || isSubmitting) return;
    onQuickCreate(t);
    setQuickTitle("");
  };

  return (
    <SidebarMenuItem className="group/repo">
      <div className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-sidebar-accent/70">
        <span className="size-1 shrink-0 rounded-full bg-muted-foreground/50" />
        <span className="min-w-0 flex-1 truncate" title={repo.url}>{name}</span>
        <Tooltip>
          <TooltipTrigger
            render={<span role="button" />}
            className="hidden size-4 shrink-0 items-center justify-center rounded-sm text-muted-foreground group-hover/repo:flex hover:text-foreground hover:bg-sidebar-accent"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              onOpenModal();
            }}
          >
            <Plus className="size-3" />
          </TooltipTrigger>
          <TooltipContent side="top" sideOffset={4}>New issue in {name}</TooltipContent>
        </Tooltip>
      </div>
      <div className="ml-3 mr-1 hidden group-hover/repo:block focus-within:!block">
        <div className="flex items-center gap-1 rounded-md border border-transparent bg-transparent px-1.5 py-0.5 text-xs hover:border-border focus-within:border-border focus-within:bg-background">
          <Plus className="size-3 shrink-0 text-muted-foreground" />
          <input
            type="text"
            value={quickTitle}
            onChange={(e) => setQuickTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                submit();
              } else if (e.key === "Escape") {
                e.currentTarget.blur();
                setQuickTitle("");
              }
            }}
            placeholder={`Quick add in ${name}…`}
            disabled={isSubmitting}
            className="min-w-0 flex-1 bg-transparent text-xs text-foreground placeholder:text-muted-foreground focus:outline-none disabled:opacity-60"
          />
        </div>
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
  const { pathname, push } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const userId = useAuthStore((s) => s.user?.id);
  const logout = useLogout();
  const workspace = useCurrentWorkspace();
  const p = useWorkspacePaths();
  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const { data: myInvitations = [] } = useQuery(myInvitationListOptions());

  const wsId = workspace?.id;
  const { data: inboxItems = [] } = useQuery({
    queryKey: wsId ? inboxKeys.list(wsId) : ["inbox", "disabled"],
    queryFn: () => api.listInbox(),
    enabled: !!wsId,
  });
  const unreadCount = React.useMemo(
    () => deduplicateInboxItems(inboxItems).filter((i) => !i.read).length,
    [inboxItems],
  );
  const hasRuntimeUpdates = useMyRuntimesNeedUpdate(wsId);
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId ?? "", userId ?? ""),
    enabled: !!wsId && !!userId,
  });
  const { data: projects = [] } = useQuery({
    ...projectListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const workspaceRepos = workspace?.repos ?? [];
  const deletePin = useDeletePin();
  const reorderPins = useReorderPins();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const oldIndex = pinnedItems.findIndex((p) => p.id === active.id);
      const newIndex = pinnedItems.findIndex((p) => p.id === over.id);
      if (oldIndex === -1 || newIndex === -1) return;
      const reordered = arrayMove(pinnedItems, oldIndex, newIndex);
      reorderPins.mutate(reordered);
    },
    [pinnedItems, reorderPins],
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
        // Auto-fill project when on a project detail page
        const projectMatch = pathname.match(/^\/[^/]+\/projects\/([^/]+)$/);
        const data = projectMatch ? { project_id: projectMatch[1] } : undefined;
        useModalStore.getState().open("create-issue", data);
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
                onClick={() => useModalStore.getState().open("create-issue")}
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

          {pinnedItems.length > 0 && (
            <Collapsible defaultOpen>
              <SidebarGroup className="group/pinned">
                <SidebarGroupLabel
                  render={<CollapsibleTrigger />}
                  className="group/trigger cursor-pointer hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground"
                >
                  <span>Pinned</span>
                  <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
                  <span className="ml-auto text-[10px] text-muted-foreground opacity-0 transition-opacity group-hover/pinned:opacity-100">{pinnedItems.length}</span>
                </SidebarGroupLabel>
                <CollapsibleContent>
                  <SidebarGroupContent>
                    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
                      <SortableContext items={pinnedItems.map((p) => p.id)} strategy={verticalListSortingStrategy}>
                        <SidebarMenu className="gap-0.5">
                          {pinnedItems.map((pin: PinnedItem) => (
                            <SortablePinItem
                              key={pin.id}
                              pin={pin}
                              href={pin.item_type === "issue" ? p.issueDetail(pin.item_id) : p.projectDetail(pin.item_id)}
                              pathname={pathname}
                              onUnpin={() => deletePin.mutate({ itemType: pin.item_type, itemId: pin.item_id })}
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
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{item.label}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          {projects.length > 0 && (
            <SidebarGroup className="group/projects">
              <SidebarGroupLabel>
                <span>Projects</span>
                <span className="ml-auto text-[10px] text-muted-foreground opacity-0 transition-opacity group-hover/projects:opacity-100">
                  {projects.length}
                </span>
              </SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu className="gap-0.5">
                  {projects.map((proj) => (
                    <ProjectSidebarRow
                      key={proj.id}
                      project={proj}
                      href={p.projectDetail(proj.id)}
                      isActive={pathname === p.projectDetail(proj.id)}
                      repos={workspaceRepos.filter((r) => r.project_id === proj.id)}
                    />
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          )}

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
