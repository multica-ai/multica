import { useMemo, type ReactNode } from 'react';
import { remapTaskCenterPath } from '@multica/core/issues/task-center';
import {
  NavigationProvider,
  useNavigation,
  type NavigationAdapter,
} from '@multica/views/navigation';
import { TagWorkspaceRoute } from './tag-workspace-route';

export function createTaskCenterNavigationAdapter(
  navigation: NavigationAdapter,
): NavigationAdapter {
  const remap = (path: string) => remapTaskCenterPath(path);
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

function TaskCenterNavigationBoundary({ children }: { children: ReactNode }) {
  const navigation = useNavigation();
  const taskNavigation = useMemo(
    () => createTaskCenterNavigationAdapter(navigation),
    [navigation],
  );

  return <NavigationProvider value={taskNavigation}>{children}</NavigationProvider>;
}

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
        <TaskCenterNavigationBoundary>{children}</TaskCenterNavigationBoundary>
      </main>
    </TagWorkspaceRoute>
  );
}
