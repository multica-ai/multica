// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const state = vi.hoisted(() => ({
  pathname: '/studio-a/chat',
  connectionState: 'connected',
  pushed: [] as string[],
  closeMobile: vi.fn(),
  openCreateTask: vi.fn(),
  floatingChatMounts: 0,
  globalShortcutMounts: 0,
  workspaces: [
    { id: 'workspace-a', slug: 'studio-a', name: 'Studio A' },
    { id: 'workspace-b', slug: 'studio-b', name: 'Studio B' },
  ],
}));

vi.mock('@multica/views/chat', () => ({
  FloatingChat: () => {
    state.floatingChatMounts += 1;
    return <div data-testid="floating-chat" />;
  },
}));

vi.mock('@multica/views/layout', () => ({
  GlobalShortcuts: () => {
    state.globalShortcutMounts += 1;
    return null;
  },
}));

vi.mock('@multica/core/paths', () => ({
  useCurrentWorkspace: () => state.workspaces[0],
}));

vi.mock('@multica/core/workspace', () => ({
  workspaceListOptions: () => ({ queryKey: ['workspaces', 'list'] }),
}));

vi.mock('@multica/core/realtime', () => ({
  useWS: () => ({ connectionState: state.connectionState }),
}));

vi.mock('@multica/core/issues/stores/create-mode-store', () => ({
  openCreateIssueWithPreference: state.openCreateTask,
}));

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({ data: state.workspaces }),
}));

vi.mock('@multica/views/navigation', () => ({
  useNavigation: () => ({
    pathname: state.pathname,
    push: (path: string) => state.pushed.push(path),
  }),
  AppLink: ({ href, children, onClick, ...props }: React.ComponentProps<'a'>) => (
    <a
      href={href}
      onClick={(event) => {
        event.preventDefault();
        onClick?.(event);
      }}
      {...props}
    >
      {children}
    </a>
  ),
}));

vi.mock('@multica/views/workspace/workspace-avatar', () => ({
  WorkspaceAvatar: ({ name }: { name: string }) => <span>{name.slice(0, 1)}</span>,
}));

vi.mock('@multica/ui/components/ui/sidebar', () => ({
  SidebarProvider: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  Sidebar: ({ children }: { children: React.ReactNode }) => <aside>{children}</aside>,
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <header>{children}</header>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarFooter: ({ children }: { children: React.ReactNode }) => <footer>{children}</footer>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <section>{children}</section>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <ul>{children}</ul>,
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <li>{children}</li>,
  SidebarMenuButton: ({
    children,
    render,
    ...props
  }: {
    children: React.ReactNode;
    render?: React.ReactElement;
  } & React.ComponentProps<'button'>) =>
    render ? (
      <a
        href={(render.props as { href: string }).href}
        className={props.className}
        onClick={(event) => {
          event.preventDefault();
          props.onClick?.(event as unknown as React.MouseEvent<HTMLButtonElement>);
        }}
      >
        {children}
      </a>
    ) : (
      <button {...props}>{children}</button>
    ),
  SidebarInset: ({ children }: { children: React.ReactNode }) => <main>{children}</main>,
  SidebarRail: () => null,
  SidebarTrigger: (props: React.ComponentProps<'button'>) => <button {...props}>Open Tag navigation</button>,
  useSidebar: () => ({ setOpenMobile: state.closeMobile }),
}));

vi.mock('@multica/ui/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactElement }) => render,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick, ...props }: { children: React.ReactNode; onClick?: () => void } & React.ComponentProps<'button'>) => <button onClick={onClick} {...props}>{children}</button>,
}));

import { TagWorkspaceShell } from './tag-workspace-shell';

beforeEach(() => {
  state.pathname = '/studio-a/chat';
  state.pushed = [];
  state.connectionState = 'connected';
  state.closeMobile.mockClear();
  state.openCreateTask.mockClear();
  state.floatingChatMounts = 0;
  state.globalShortcutMounts = 0;
});

afterEach(cleanup);

describe('TagWorkspaceShell', () => {
  it('provides exactly one Floating Chat mount point inside the Workspace Shell', () => {
    render(<TagWorkspaceShell><div>Workspace content</div></TagWorkspaceShell>);

    expect(screen.getAllByTestId('floating-chat')).toHaveLength(1);
    expect(state.floatingChatMounts).toBe(1);
    expect(state.globalShortcutMounts).toBe(1);
  });

  it('shows approved links and only deferred modules as migrating', () => {
    render(<TagWorkspaceShell><div>Chat content</div></TagWorkspaceShell>);

    expect(screen.getByRole('link', { name: 'Chat' }).getAttribute('href')).toBe('/studio-a/chat');
    expect(screen.getByRole('link', { name: 'Tasks' }).getAttribute('href')).toBe('/studio-a/issues');
    expect(screen.getByRole('link', { name: 'Agents' }).getAttribute('href')).toBe('/studio-a/agents');
    expect(screen.getByRole('link', { name: 'Runtimes' }).getAttribute('href')).toBe('/studio-a/runtimes');
    expect(screen.getByText('Projects').closest('button')?.disabled).toBe(true);
    for (const removed of ['Inbox', 'My Tasks', 'Skills', 'Squads', 'Autopilots', 'Analytics']) {
      expect(screen.queryByText(removed)).toBeNull();
    }
    expect(screen.getAllByText('Migrating').length).toBeGreaterThan(0);
    expect(screen.getByText('Chat content')).toBeTruthy();
  });

  it('offers the VIBES return and mobile navigation as 44px controls', () => {
    render(<TagWorkspaceShell><div /></TagWorkspaceShell>);

    for (const link of screen.getAllByRole('link', { name: 'Return to VIBES' })) {
      expect(link.className).toContain('min-h-11');
      expect(link.className).toContain('min-w-11');
    }
    expect(screen.getByRole('button', { name: 'Open Tag navigation' }).className).toContain('min-h-11');
  });

  it('keeps Shell-level Search honest and opens the existing New Task flow', () => {
    render(<TagWorkspaceShell><div /></TagWorkspaceShell>);

    expect(screen.getByRole('button', { name: 'Search (Migrating)' }).hasAttribute('disabled')).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'New Task' }));
    expect(state.openCreateTask).toHaveBeenCalledOnce();
  });

  it('switches Workspace inside the current live module', () => {
    render(<TagWorkspaceShell><div /></TagWorkspaceShell>);

    fireEvent.click(screen.getByRole('button', { name: 'Switch to Studio B' }));

    expect(state.pushed).toEqual(['/studio-b/chat']);
  });

  it('keeps the Shell visible while realtime is reconnecting', () => {
    state.connectionState = 'reconnecting';

    render(<TagWorkspaceShell><div>Current Workspace content</div></TagWorkspaceShell>);

    expect(screen.getByRole('status').textContent).toContain(
      'Connection lost. Reconnecting'
    );
    expect(screen.getByText('Current Workspace content')).toBeTruthy();
  });

  it('announces realtime recovery without replacing Workspace content', async () => {
    state.connectionState = 'reconnecting';
    const view = render(
      <TagWorkspaceShell><div>Current Workspace content</div></TagWorkspaceShell>
    );

    state.connectionState = 'connected';
    view.rerender(
      <TagWorkspaceShell><div>Current Workspace content</div></TagWorkspaceShell>
    );

    expect((await screen.findByRole('status')).textContent).toContain(
      'Connection restored'
    );
    expect(screen.getByText('Current Workspace content')).toBeTruthy();
  });

  it('closes the mobile drawer even when the active module is selected again', () => {
    render(<TagWorkspaceShell><div /></TagWorkspaceShell>);
    state.closeMobile.mockClear();

    fireEvent.click(screen.getByRole('link', { name: 'Chat' }));

    expect(state.closeMobile).toHaveBeenCalledWith(false);
  });
});
