import { createFileRoute } from '@tanstack/react-router';
import { RuntimeDetailPage } from '@multica/views/runtimes';
import { RuntimeWorkspaceRoute } from '@/workspace/runtime-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/runtimes/$machineId')({
  ssr: false,
  component: TagWorkspaceRuntimeDetailRoute,
});

function TagWorkspaceRuntimeDetailRoute() {
  const { workspaceSlug, machineId } = Route.useParams();
  return (
    <RuntimeWorkspaceRoute
      workspaceSlug={workspaceSlug}
      resetKeys={[machineId]}
    >
      <RuntimeDetailPage runtimeId={machineId} />
    </RuntimeWorkspaceRoute>
  );
}
