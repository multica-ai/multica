"use client";

import React from "react";
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
import { useQuery } from "@tanstack/react-query";
import { inboxKeys, deduplicateInboxItems } from "@multica/core/inbox/queries";
import { api } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import { useMyRuntimesNeedUpdate } from "@multica/core/runtimes/hooks";
import { useAppLocale, locales, localeLabels } from "../i18n";

import type { LucideIcon } from "lucide-react";

type NavItem = {
  href: string;
  navKey: string;
  icon: LucideIcon;
};

const personalNav: NavItem[] = [
  { href: "/inbox", navKey: "inbox", icon: Inbox },
  { href: "/my-issues", navKey: "myIssues", icon: CircleUser },

];

const workspaceNav: NavItem[] = [
  { href: "/issues", navKey: "issues", icon: ListTodo },
  { href: "/projects", navKey: "projects", icon: FolderKanban },
  { href: "/agents", navKey: "agents", icon: Bot },
];

const configureNav: NavItem[] = [
  { href: "/runtimes", navKey: "runtimes", icon: Monitor },
  { href: "/skills", navKey: "skills", icon: BookOpenText },
  { href: "/settings", navKey: "settings", icon: Settings },
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
  const { locale, t, setLocale } = useAppLocale();

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

  const logout = () => {
    authLogout();
    useWorkspaceStore.getState().clearWorkspace();
  };

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
                      {t.nav.workspaces}
                      <Tooltip>
                        <TooltipTrigger
                          className="ml-auto opacity-0 group-hover/ws-section:opacity-100 transition-opacity rounded hover:bg-accent p-0.5"
                          onClick={() => useModalStore.getState().open("create-workspace")}
                        >
                          <Plus className="h-3.5 w-3.5" />
                        </TooltipTrigger>
                        <TooltipContent side="right">
                          {t.nav.createWorkspace}
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
                      {t.nav.logOut}
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
                <span>{t.nav.newIssue}</span>
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
                  const label = t.nav[item.navKey as keyof typeof t.nav];
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={item.href} />}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{label}</span>
                        {item.navKey === "inbox" && unreadCount > 0 && (
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
            <SidebarGroupLabel>{t.nav.workspaceLabel}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {workspaceNav.map((item) => {
                  const isActive = pathname === item.href;
                  const label = t.nav[item.navKey as keyof typeof t.nav];
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={item.href} />}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{label}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <SidebarGroup>
            <SidebarGroupLabel>{t.nav.configureLabel}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {configureNav.map((item) => {
                  const isActive = pathname === item.href;
                  const label = t.nav[item.navKey as keyof typeof t.nav];
                  return (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        isActive={isActive}
                        render={<AppLink href={item.href} />}
                        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                      >
                        <item.icon />
                        <span>{label}</span>
                        {item.navKey === "runtimes" && hasRuntimeUpdates && (
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
          <div className="flex items-center justify-center gap-1 px-4 py-1">
            {locales.map((l, i) => (
              <button
                key={l}
                onClick={() => setLocale(l)}
                className={cn(
                  "px-1.5 py-0.5 text-[11px] font-medium transition-colors",
                  l === locale ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                  i > 0 && "border-l border-border pl-1.5"
                )}
              >
                {localeLabels[l]}
              </button>
            ))}
          </div>
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
                    {t.nav.logOut}
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
