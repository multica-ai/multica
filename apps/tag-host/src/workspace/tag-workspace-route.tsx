import type { ReactNode } from 'react';
import { ErrorBoundary } from '@multica/ui/components/common/error-boundary';
import { ModalRegistry } from '@multica/views/modals/registry';
import { TagHostProviders } from '@/platform/tag-host-providers';
import { TagWorkspaceShell } from './tag-workspace-shell';
import { WorkspaceGate } from './workspace-gate';

export function TagWorkspaceRoute({
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
          <TagWorkspaceShell>{children}</TagWorkspaceShell>
          <ModalRegistry />
        </ErrorBoundary>
      </WorkspaceGate>
    </TagHostProviders>
  );
}
