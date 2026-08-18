import type { ReactNode } from 'react';
import { TagWorkspaceRoute } from './tag-workspace-route';

/**
 * Runtime routes deliberately keep the original Multica views in the Tag
 * workspace shell. This is a host-only seam: runtime data, mutations, query
 * caches, and realtime recovery continue to come from Core and the daemon.
 */
export function RuntimeWorkspaceRoute({
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
      <main className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-background text-foreground">
        {children}
      </main>
    </TagWorkspaceRoute>
  );
}
