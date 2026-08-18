import type { ReactNode } from 'react';
import { ErrorBoundary } from '@multica/ui/components/common/error-boundary';
import { ModalRegistry } from '@multica/views/modals/registry';
import { TagHostProviders } from '@/platform/tag-host-providers';
import { WorkspaceGate } from './workspace-gate';

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
    <TagHostProviders>
      <WorkspaceGate workspaceSlug={workspaceSlug}>
        <ErrorBoundary resetKeys={resetKeys}>
          <main className="relative flex h-svh min-h-0 flex-col bg-background text-foreground">
            {children}
          </main>
          <ModalRegistry />
        </ErrorBoundary>
      </WorkspaceGate>
    </TagHostProviders>
  );
}
