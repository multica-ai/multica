import { useMemo, type ReactNode } from 'react';
import {
  AppLink,
  NavigationProvider,
  useNavigation,
  type NavigationAdapter,
} from '@multica/views/navigation';
import { cn } from '@multica/ui/lib/utils';
import { agentCenterPath, remapAgentCenterPath } from './agent-center';
import { TagWorkspaceRoute } from './tag-workspace-route';

export function createAgentCenterNavigationAdapter(
  navigation: NavigationAdapter,
): NavigationAdapter {
  const remap = (path: string) => remapAgentCenterPath(path);
  return {
    ...navigation,
    push: (path) => navigation.push(remap(path)),
    replace: (path) => navigation.replace(remap(path)),
    openInNewTab: navigation.openInNewTab
      ? (path, title, options) =>
          navigation.openInNewTab?.(remap(path), title, options)
      : undefined,
    getShareableUrl: (path) => navigation.getShareableUrl(remap(path)),
    resolveHref: navigation.resolveHref
      ? (path) => navigation.resolveHref?.(remap(path)) ?? remap(path)
      : remap,
    prefetch: navigation.prefetch
      ? (path) => navigation.prefetch?.(remap(path))
      : undefined,
  };
}

function AgentCenterBoundary({ children }: { children: ReactNode }) {
  const navigation = useNavigation();
  const agentNavigation = useMemo(
    () => createAgentCenterNavigationAdapter(navigation),
    [navigation],
  );
  return (
    <NavigationProvider value={agentNavigation}>{children}</NavigationProvider>
  );
}

function AgentCenterNavigation({ workspaceSlug }: { workspaceSlug: string }) {
  const { pathname } = useNavigation();
  const activeSection = pathname.includes('/agents/teams') ? 'teams' : 'roles';

  return (
    <nav
      aria-label="Agents"
      className="flex h-12 shrink-0 items-end gap-1 border-b px-4"
    >
      {(['roles', 'teams'] as const).map((section) => (
        <AppLink
          key={section}
          href={agentCenterPath(workspaceSlug, section)}
          aria-current={activeSection === section ? 'page' : undefined}
          className={cn(
            'flex h-full items-center border-b-2 px-2.5 text-caption font-medium transition-colors',
            activeSection === section
              ? 'border-foreground text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground',
          )}
        >
          {section === 'roles' ? 'Roles' : 'Teams'}
        </AppLink>
      ))}
    </nav>
  );
}

export function AgentWorkspaceRoute({
  workspaceSlug,
  resetKeys,
  children,
}: {
  workspaceSlug: string;
  resetKeys?: unknown[];
  children: ReactNode;
}) {
  return (
    <TagWorkspaceRoute workspaceSlug={workspaceSlug} resetKeys={resetKeys}>
      <AgentCenterBoundary>
        <main className="flex h-full min-h-0 flex-col bg-background text-foreground">
          <AgentCenterNavigation workspaceSlug={workspaceSlug} />
          <div className="flex min-h-0 flex-1 flex-col">{children}</div>
        </main>
      </AgentCenterBoundary>
    </TagWorkspaceRoute>
  );
}
