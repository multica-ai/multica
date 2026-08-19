import { useEffect, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@multica/core/auth';
import { WorkspaceSlugProvider } from '@multica/core/paths';
import {
  getCurrentSlug,
  setCurrentWorkspace,
} from '@multica/core/platform';
import { workspaceBySlugOptions } from '@multica/core/workspace';
import { Button } from '@multica/ui/components/ui/button';

export function WorkspaceGate({
  workspaceSlug,
  children,
}: {
  workspaceSlug: string;
  children: ReactNode;
}) {
  const user = useAuthStore((state) => state.user);
  const isAuthLoading = useAuthStore((state) => state.isLoading);
  const { data: workspace, isError, refetch } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug),
    enabled: Boolean(user),
  });

  if (workspace) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  } else if (getCurrentSlug() !== null) {
    setCurrentWorkspace(null, null);
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
  if (!user) return <TagHostStatus label="VIBES Tag access needs to be refreshed" />;
  if (isError) {
    return (
      <TagHostStatus label="Could not load workspace">
        <Button className="min-h-11" onClick={() => void refetch()}>
          Retry
        </Button>
      </TagHostStatus>
    );
  }
  if (workspace === undefined) {
    return <TagHostStatus label="Resolving workspace" />;
  }
  if (workspace === null) {
    return <TagHostStatus label="Workspace unavailable or access denied" />;
  }

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      {children}
    </WorkspaceSlugProvider>
  );
}

function TagHostStatus({
  label,
  children,
}: {
  label: string;
  children?: ReactNode;
}) {
  return (
    <main className="grid min-h-svh place-items-center bg-background text-foreground">
      <div className="flex flex-col items-center gap-4 text-center">
        <p className="text-body text-muted-foreground">{label}</p>
        {children}
        <a
          aria-label="Return to VIBES"
          className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-md px-4 text-body font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
          href="/"
        >
          Return to VIBES
        </a>
      </div>
    </main>
  );
}
