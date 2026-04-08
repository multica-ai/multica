"use client";

import React from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
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
} from "lucide-react";
import { WorkspaceAvatar } from "@/features/workspace";
import { useIssueDraftStore } from "@/features/issues/stores/draft-store";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarFooter,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Drawer,
  DrawerTrigger,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerDescription,
  DrawerClose,
} from "@/components/ui/drawer";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { useQuery } from "@tanstack/react-query";
import { inboxKeys, deduplicateInboxItems } from "@core/inbox/queries";
import { api } from "@/shared/api";
import { useModalStore } from "@/features/modals";

const primaryNav = [
  { href: "/inbox", label: "Inbox", icon: Inbox },
  { href: "/my-issues", label: "My Issues", icon: CircleUser },
  { href: "/issues", label: "Issues", icon: ListTodo },
];

const workspaceNav = [
  { href: "/agents", label: "Agents", icon: Bot },
  { href: "/runtimes", label: "Runtimes", icon: Monitor },
  { href: "/skills", label: "Skills", icon: BookOpenText },
  { href: "/settings", label: "Settings", icon: Settings },
];

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
}

interface WorkspaceSwitcherProps {
  workspace: { id: string; name: string } | null;
  workspaces: readonly { id: string; name: string }[];
  user: { email?: string } | null;
  onSwitch: (id: string) => void;
  onLogout: () => void;
}

function WorkspaceSwitcherTrigger({
  workspace,
}: Pick<WorkspaceSwitcherProps, "workspace">) {
  return (
    <>
      <WorkspaceAvatar name={workspace?.name ?? "M"} size="sm" />
      <span className="flex-1 truncate font-medium">
        {workspace?.name ?? "Multica"}
      </span>
      <ChevronDown className="size-3 text-muted-foreground" />
    </>
  );
}

function DesktopWorkspaceSwitcher(props: WorkspaceSwitcherProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <SidebarMenuButton>
            <WorkspaceSwitcherTrigger workspace={props.workspace} />
          </SidebarMenuButton>
        }
      />
      <DropdownMenuContent
        className="w-52"
        align="start"
        side="bottom"
        sideOffset={4}
      >
        <DropdownMenuGroup>
          <DropdownMenuLabel className="text-xs text-muted-foreground">
            {props.user?.email}
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
          {props.workspaces.map((ws) => (
            <DropdownMenuItem
              key={ws.id}
              onClick={() => {
                if (ws.id !== props.workspace?.id) {
                  props.onSwitch(ws.id);
                }
              }}
            >
              <WorkspaceAvatar name={ws.name} size="sm" />
              <span className="flex-1 truncate">{ws.name}</span>
              {ws.id === props.workspace?.id && (
                <Check className="h-3.5 w-3.5 text-primary" />
              )}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem variant="destructive" onClick={props.onLogout}>
            <LogOut className="h-3.5 w-3.5" />
            Log out
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function MobileWorkspaceSwitcher(props: WorkspaceSwitcherProps) {
  return (
    <Drawer>
      <DrawerTrigger asChild>
        <SidebarMenuButton>
          <WorkspaceSwitcherTrigger workspace={props.workspace} />
        </SidebarMenuButton>
      </DrawerTrigger>
      <DrawerContent>
        <DrawerHeader className="sr-only">
          <DrawerTitle>Switch workspace</DrawerTitle>
          <DrawerDescription>Choose a workspace to switch to</DrawerDescription>
        </DrawerHeader>
        <div className="px-4 pb-6">
          <div className="mb-2 text-xs text-muted-foreground">
            {props.user?.email}
          </div>
          <div className="my-2 h-px bg-border" />
          <DrawerClose asChild>
            <button
              className="mb-2 flex w-full items-center gap-2 rounded-sm text-xs text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring outline-none"
              onClick={() => {
                useModalStore.getState().open("create-workspace");
              }}
            >
              <Plus className="h-3.5 w-3.5" />
              Create workspace
            </button>
          </DrawerClose>
          <div className="my-2 h-px bg-border" />
          <div className="max-h-[60vh] overflow-y-auto pr-1">
            <div className="px-2 py-1.5 text-xs text-muted-foreground">
              Workspaces
            </div>
            {props.workspaces.map((ws) => (
              <DrawerClose key={ws.id} asChild>
                <button
                  className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring outline-none"
                  onClick={() => {
                    if (ws.id !== props.workspace?.id) {
                      props.onSwitch(ws.id);
                    }
                  }}
                >
                  <WorkspaceAvatar name={ws.name} size="sm" />
                  <span className="flex-1 truncate">{ws.name}</span>
                  {ws.id === props.workspace?.id && (
                    <Check className="h-3.5 w-3.5 text-primary" />
                  )}
                </button>
              </DrawerClose>
            ))}
            <div className="my-1 h-px bg-border" />
            <DrawerClose asChild>
              <button
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm text-destructive hover:bg-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring outline-none"
                onClick={props.onLogout}
              >
                <LogOut className="h-3.5 w-3.5" />
                Log out
              </button>
            </DrawerClose>
          </div>
        </div>
      </DrawerContent>
    </Drawer>
  );
}

export function AppSidebar() {
  const pathname = usePathname();
  const router = useRouter();
  const { isMobile } = useSidebar();
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

  const logout = () => {
    router.push("/");
    authLogout();
    useWorkspaceStore.getState().clearWorkspace();
  };

  const handleSwitchWorkspace = (id: string) => {
    router.push("/issues");
    switchWorkspace(id);
  };

  const switcherProps: WorkspaceSwitcherProps = {
    workspace: workspace
      ? { id: workspace.id, name: workspace.name }
      : null,
    workspaces,
    user,
    onSwitch: handleSwitchWorkspace,
    onLogout: logout,
  };

  return (
      <Sidebar variant="inset">
        {/* Workspace Switcher */}
        <SidebarHeader className="py-3">
          <div className="flex items-center gap-4">
            <SidebarMenu className="min-w-0 flex-1">
              <SidebarMenuItem>
                {isMobile ? (
                  <MobileWorkspaceSwitcher {...switcherProps} />
                ) : (
                  <DesktopWorkspaceSwitcher {...switcherProps} />
                )}
              </SidebarMenuItem>
            </SidebarMenu>
            <Tooltip>
              <TooltipTrigger
                className="relative flex h-7 w-7 items-center justify-center rounded-lg bg-background text-foreground shadow-sm hover:bg-accent"
                onClick={() => useModalStore.getState().open("create-issue")}
              >
                <SquarePen className="size-3.5" />
                <DraftDot />
              </TooltipTrigger>
              <TooltipContent side="bottom">New issue</TooltipContent>
            </Tooltip>
          </div>
        </SidebarHeader>

        {/* Navigation */}
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {primaryNav.map((item) => {
                  const isActive = pathname === item.href;
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<Link href={item.href} />}
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
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {workspaceNav.map((item) => {
                  const isActive = pathname === item.href;
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<Link href={item.href} />}
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
        </SidebarContent>
        <SidebarFooter />
        <SidebarRail />
      </Sidebar>
  );
}
