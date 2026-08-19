import type { ReactNode } from 'react';
import { TagWorkspaceRoute } from './tag-workspace-route';

export function TaskWorkspaceRoute({
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
      <main className="relative flex h-full min-h-0 flex-col bg-background text-foreground">
        {children}
      </main>
    </TagWorkspaceRoute>
  );
}
