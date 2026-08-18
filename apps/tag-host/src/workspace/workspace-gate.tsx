import { useEffect, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@multica/core/auth';
import { WorkspaceSlugProvider } from '@multica/core/paths';
import {
  getCurrentSlug,
  setCurrentWorkspace,
} from '@multica/core/platform';
import { workspaceBySlugOptions } from '@multica/core/workspace';

export function WorkspaceGate({
  workspaceSlug,
  children,
}: {
  workspaceSlug: string;
  children: ReactNode;
}) {
  const user = useAuthStore((state) => state.user);
  const isAuthLoading = useAuthStore((state) => state.isLoading);
  const { data: workspace } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug),
    enabled: Boolean(user),
  });

  if (workspace) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  }

  useEffect(
    () => () => {
      if (getCurrentSlug() === workspaceSlug) {
        setCurrentWorkspace(null, null);
      }
    },
    [workspaceSlug]
  );

  if (isAuthLoading) return <TagHostStatus label="Restoring local session" />;
  if (!user) return <TagHostStatus label="Existing Multica session required" />;
  if (workspace === undefined) {
    return <TagHostStatus label="Resolving workspace" />;
  }
  if (workspace === null) return <TagHostStatus label="Workspace unavailable" />;

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      {children}
    </WorkspaceSlugProvider>
  );
}

function TagHostStatus({ label }: { label: string }) {
  return (
    <main className="grid min-h-svh place-items-center bg-background text-foreground">
      <p className="text-body text-muted-foreground">{label}</p>
    </main>
  );
}
