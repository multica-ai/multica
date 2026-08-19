import type { ReactNode } from 'react';
import { paths } from '@multica/core/paths';
import { cn } from '@multica/ui/lib/utils';
import { useT } from '@multica/views/i18n';
import { AppLink, useNavigation } from '@multica/views/navigation';
import { CHAT_WORKSPACE_TABS } from './workspace-shell-model';
import { TagWorkspaceRoute } from './tag-workspace-route';

function ChatWorkspaceNavigation({ workspaceSlug }: { workspaceSlug: string }) {
  const { pathname } = useNavigation();
  const { t } = useT('chat');
  const workspacePaths = paths.workspace(workspaceSlug);
  const filesPath = workspacePaths.chatFiles();
  const activeTab = pathname === filesPath ? 'files' : 'chat';

  return (
    <nav
      aria-label={t(($) => $.files.navigation_label)}
      className="flex h-12 shrink-0 items-end gap-1 overflow-x-auto border-b px-4"
    >
      {CHAT_WORKSPACE_TABS.map((tab) => {
        const active = activeTab === tab.key;
        const href = tab.key === 'files' ? filesPath : workspacePaths.chat();
        return (
          <AppLink
            key={tab.key}
            href={href}
            aria-current={active ? 'page' : undefined}
            className={cn(
              'flex h-full min-w-16 shrink-0 items-center justify-center whitespace-nowrap border-b-2 px-2.5 text-caption font-medium transition-colors',
              active
                ? 'border-foreground text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground',
            )}
          >
            {tab.key === 'chat'
              ? t(($) => $.page.title)
              : t(($) => $.files.title)}
          </AppLink>
        );
      })}
    </nav>
  );
}

export function ChatWorkspaceRoute({
  workspaceSlug,
  children,
}: {
  workspaceSlug: string;
  children: ReactNode;
}) {
  return (
    <TagWorkspaceRoute workspaceSlug={workspaceSlug}>
      <main className="flex h-full min-h-0 flex-col bg-background text-foreground">
        <ChatWorkspaceNavigation workspaceSlug={workspaceSlug} />
        <div className="flex min-h-0 flex-1 flex-col">{children}</div>
      </main>
    </TagWorkspaceRoute>
  );
}
