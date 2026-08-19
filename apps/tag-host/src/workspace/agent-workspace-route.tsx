import type { ReactNode } from 'react';
import { useT } from '@multica/views/i18n';
import { AppLink, useNavigation } from '@multica/views/navigation';
import { cn } from '@multica/ui/lib/utils';
import { agentCenterPath } from './agent-center';
import { TagWorkspaceRoute } from './tag-workspace-route';

function AgentCenterNavigation({ workspaceSlug }: { workspaceSlug: string }) {
  const { pathname } = useNavigation();
  const { t } = useT('agents');
  const activeSection = pathname.includes('/agents/teams') ? 'teams' : 'roles';

  return (
    <nav
      aria-label={t(($) => $.role_center.navigation_label)}
      className="flex h-12 shrink-0 items-end gap-1 overflow-x-auto border-b px-4"
    >
      {(['roles', 'teams'] as const).map((section) => (
        <AppLink
          key={section}
          href={agentCenterPath(workspaceSlug, section)}
          aria-current={activeSection === section ? 'page' : undefined}
          className={cn(
            'flex h-full shrink-0 items-center whitespace-nowrap border-b-2 px-2.5 text-caption font-medium transition-colors',
            activeSection === section
              ? 'border-foreground text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground',
          )}
        >
          {section === 'roles'
            ? t(($) => $.role_center.roles)
            : t(($) => $.role_center.teams)}
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
      <main className="flex h-full min-h-0 flex-col bg-background text-foreground">
        <AgentCenterNavigation workspaceSlug={workspaceSlug} />
        <div className="flex min-h-0 flex-1 flex-col">{children}</div>
      </main>
    </TagWorkspaceRoute>
  );
}
