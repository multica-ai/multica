"use client";

import React, { useEffect } from "react";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../navigation";
import {
  Inbox,
  ListTodo,
  Bot,
  Monitor,
  ChevronDown,
  Settings,
  LogOut,
  Plus,
  Check,
  BookOpenText,
  SquarePen,
  CircleUser,
  FolderKanban,
  Ellipsis,
} from "lucide-react";
import { WorkspaceAvatar } from "../workspace/workspace-avatar";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
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
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { inboxKeys, deduplicateInboxItems } from "@multica/core/inbox/queries";
import { api } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import { useMyRuntimesNeedUpdate } from "@multica/core/runtimes/hooks";

const personalNav = [
  { href: "/inbox", label: "Inbox", icon: Inbox },
  { href: "/my-issues", label: "My Issues", icon: CircleUser },
];

const workspaceNav = [
  { href: "/issues", label: "Issues", icon: ListTodo },
  { href: "/projects", label: "Projects", icon: FolderKanban },
  { href: "/agents", label: "Agents", icon: Bot },
];

const configureNav = [
  { href: "/runtimes", label: "Runtimes", icon: Monitor },
  { href: "/skills", label: "Skills", icon: BookOpenText },
  { href: "/settings", label: "Settings", icon: Settings },
];

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
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
  const authLogout = useAuthStore((s) => s.logout);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const workspaces = useWorkspaceStore((s) => s.workspaces);
  const switchWorkspace = useWorkspaceStore((s) => s.switchWorkspace);

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

  const queryClient = useQueryClient();
  const logout = () => {
    queryClient.clear();
    authLogout();
    useWorkspaceStore.getState().clearWorkspace();
  };

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
        const projectMatch = pathname.match(/^\/projects\/([^/]+)$/);
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
                  <DropdownMenuGroup className="group/ws-section">
                    <DropdownMenuLabel className="flex items-center text-xs text-muted-foreground">
                      Workspaces
                      <Tooltip>
                        <TooltipTrigger
                          className="ml-auto opacity-0 group-hover/ws-section:opacity-100 transition-opacity rounded hover:bg-accent p-0.5"
                          onClick={() => useModalStore.getState().open("create-workspace")}
                        >
                          <Plus className="h-3.5 w-3.5" />
                        </TooltipTrigger>
                        <TooltipContent side="right">
                          Create workspace
                        </TooltipContent>
                      </Tooltip>
                    </DropdownMenuLabel>
                    {workspaces.map((ws) => (
                      <DropdownMenuItem
                        key={ws.id}
                        onClick={() => {
                          if (ws.id !== workspace?.id) {
                            push("/issues");
                            switchWorkspace(ws.id);
                          }
                        }}
                      >
                        <WorkspaceAvatar name={ws.name} size="sm" />
                        <span className="flex-1 truncate">{ws.name}</span>
                        {ws.id === workspace?.id && (
                          <Check className="h-3.5 w-3.5 text-primary" />
                        )}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuGroup>
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
                  const isActive = pathname === item.href;
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={item.href} />}
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

          <SidebarGroup>
            <SidebarGroupLabel>Workspace</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {workspaceNav.map((item) => {
                  const isActive = pathname === item.href;
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={item.href} />}
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

          <SidebarGroup>
            <SidebarGroupLabel>Configure</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {configureNav.map((item) => {
                  const isActive = pathname === item.href;
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={item.href} />}
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
            <div className="flex items-center gap-2.5 rounded-md px-2 py-1.5">
              <ActorAvatar
                name={user?.name ?? ""}
                initials={(user?.name ?? "U").charAt(0).toUpperCase()}
                avatarUrl={user?.avatar_url}
                size={28}
              />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium leading-tight">
                  {user?.name}
                </p>
                <p className="truncate text-xs text-muted-foreground leading-tight">
                  {user?.email}
                </p>
              </div>
              <DropdownMenu>
                <DropdownMenuTrigger className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors">
                  <Ellipsis className="size-4" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" side="top" sideOffset={4}>
                  <DropdownMenuItem variant="destructive" onClick={logout}>
                    <LogOut className="h-3.5 w-3.5" />
                    Log out
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>
  );
}
