import {
  useEffect,
  useRef,
  useState,
  type ComponentType,
  type ReactNode,
} from 'react';
import { useQuery } from '@tanstack/react-query';
import { useCurrentWorkspace } from '@multica/core/paths';
import { workspaceListOptions } from '@multica/core/workspace';
import { useWS } from '@multica/core/realtime';
import { openCreateIssueWithPreference } from '@multica/core/issues/stores/create-mode-store';
import {
  ArrowLeft,
  Bell,
  Bot,
  FileText,
  Gauge,
  MessageCircle,
  Search,
  Settings,
  SquarePen,
  Users,
} from 'lucide-react';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from '@multica/ui/components/ui/sidebar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@multica/ui/components/ui/dropdown-menu';
import { WorkspaceAvatar } from '@multica/views/workspace/workspace-avatar';
import { FloatingChat } from '@multica/views/chat';
import { GlobalShortcuts } from '@multica/views/layout';
import { AppLink, useNavigation } from '@multica/views/navigation';
import {
  TAG_WORKSPACE_SECTIONS,
  workspaceSwitchDestination,
} from './workspace-shell-model';

const NAV_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  chat: MessageCircle,
  tasks: FileText,
  agents: Bot,
  runtimes: Gauge,
  members: Users,
  notifications: Bell,
  settings: Settings,
};

export function TagWorkspaceShell({ children }: { children: ReactNode }) {
  return (
    <SidebarProvider
      hasExternalTrigger
      className="h-svh overflow-hidden bg-app-shell"
    >
      <GlobalShortcuts />
      <TagWorkspaceSidebar />
      <SidebarInset className="min-w-0 overflow-hidden">
        <MobileWorkspaceHeader />
        <RealtimeStatus />
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          {children}
        </div>
        <FloatingChat />
      </SidebarInset>
    </SidebarProvider>
  );
}

function RealtimeStatus() {
  const { connectionState } = useWS();
  const wasReconnecting = useRef(false);
  const [showRestored, setShowRestored] = useState(false);

  useEffect(() => {
    if (connectionState === 'reconnecting') {
      wasReconnecting.current = true;
      return;
    }
    if (connectionState !== 'connected' || !wasReconnecting.current) {
      return;
    }

    wasReconnecting.current = false;
    setShowRestored(true);
    const timer = window.setTimeout(() => setShowRestored(false), 3_000);
    return () => window.clearTimeout(timer);
  }, [connectionState]);

  const isRestored = connectionState === 'connected' && showRestored;
  if (connectionState === 'connected' && !isRestored) return null;

  const label =
    isRestored
      ? 'Connection restored'
      : connectionState === 'reconnecting'
      ? 'Connection lost. Reconnecting…'
      : 'Connecting to workspace…';

  return (
    <div
      role="status"
      aria-live="polite"
      className="shrink-0 bg-muted px-4 py-2 text-center text-caption text-muted-foreground"
    >
      {label}
    </div>
  );
}

function TagWorkspaceSidebar() {
  const workspace = useCurrentWorkspace();
  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const { pathname, push } = useNavigation();
  const { setOpenMobile } = useSidebar();

  useEffect(() => {
    setOpenMobile(false);
  }, [pathname, setOpenMobile]);

  return (
    <Sidebar variant="inset">
      <SidebarHeader className="py-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <SidebarMenuButton className="min-h-11 lg:min-h-8" />
                }
              >
                <WorkspaceAvatar
                  name={workspace?.name ?? 'Workspace'}
                  avatarUrl={workspace?.avatar_url}
                  size="sm"
                />
                <span className="min-w-0 flex-1 truncate font-medium">
                  {workspace?.name ?? 'Workspace'}
                </span>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="min-w-56">
                {workspaces.map((candidate) => (
                  <DropdownMenuItem
                    key={candidate.id}
                    aria-label={`Switch to ${candidate.name}`}
                    className="min-h-11 lg:min-h-8"
                    disabled={candidate.id === workspace?.id}
                    onClick={() =>
                      push(
                        workspaceSwitchDestination(candidate.slug, pathname)
                      )
                    }
                  >
                    <WorkspaceAvatar
                      name={candidate.name}
                      avatarUrl={candidate.avatar_url}
                      size="sm"
                    />
                    <span className="truncate">{candidate.name}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              aria-label="Search (Migrating)"
              className="min-h-11 opacity-70 lg:min-h-8"
              disabled
            >
              <Search />
              <span>Search</span>
              <span className="ml-auto text-micro text-muted-foreground">
                Migrating
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              aria-label="New Task"
              className="min-h-11 text-muted-foreground lg:min-h-8"
              onClick={() => {
                setOpenMobile(false);
                openCreateIssueWithPreference();
              }}
            >
              <SquarePen />
              <span>New Task</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        {TAG_WORKSPACE_SECTIONS.map((section) => (
          <SidebarGroup key={section.label}>
            <SidebarGroupLabel>{section.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {section.items.map((item) => {
                  const Icon = NAV_ICONS[item.key] ?? FileText;
                  const href = item.path
                    ? `/${encodeURIComponent(workspace?.slug ?? '')}/${item.path}`
                    : null;
                  const active = href
                    ? pathname === href ||
                      pathname.startsWith(`${href}/`) ||
                      (item.key === 'tasks' &&
                        pathname.split('/').filter(Boolean)[1] === 'projects')
                    : false;

                  return (
                    <SidebarMenuItem key={item.key}>
                      {href ? (
                        <SidebarMenuButton
                          isActive={active}
                          render={<AppLink href={href} />}
                          onClick={() => setOpenMobile(false)}
                          className="min-h-11 text-muted-foreground data-active:text-sidebar-accent-foreground lg:min-h-8"
                        >
                          <Icon />
                          <span>{item.label}</span>
                        </SidebarMenuButton>
                      ) : (
                        <SidebarMenuButton
                          disabled
                          className="min-h-11 opacity-70 lg:min-h-8"
                        >
                          <Icon />
                          <span>{item.label}</span>
                          <span className="ml-auto text-micro text-muted-foreground">
                            Migrating
                          </span>
                        </SidebarMenuButton>
                      )}
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter className="hidden lg:flex">
        <ReturnToVibesLink />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

function MobileWorkspaceHeader() {
  const workspace = useCurrentWorkspace();

  return (
    <header className="grid h-[calc(52px+env(safe-area-inset-top))] shrink-0 grid-cols-[44px_minmax(0,1fr)_44px] items-end border-b border-border px-2 pb-1 pt-[env(safe-area-inset-top)] lg:hidden">
      <ReturnToVibesLink compact />
      <span className="self-center truncate px-2 text-center text-label font-medium">
        {workspace?.name ?? 'Workspace'}
      </span>
      <SidebarTrigger
        aria-label="Open Tag navigation"
        className="min-h-11 min-w-11 justify-self-end"
      />
    </header>
  );
}

function ReturnToVibesLink({ compact = false }: { compact?: boolean }) {
  return (
    <a
      href="/"
      aria-label="Return to VIBES"
      className="inline-flex min-h-11 min-w-11 items-center gap-2 rounded-md text-body text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring lg:min-h-8"
    >
      <ArrowLeft className="size-4" />
      {!compact && <span>VIBES</span>}
    </a>
  );
}
