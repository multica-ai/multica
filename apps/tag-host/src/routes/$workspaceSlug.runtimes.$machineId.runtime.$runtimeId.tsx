import { createFileRoute } from '@tanstack/react-router';
import { RuntimeSettingsPage } from '@multica/views/runtimes';
import { RuntimeWorkspaceRoute } from '@/workspace/runtime-workspace-route';

export const Route = createFileRoute(
  '/$workspaceSlug/runtimes/$machineId/runtime/$runtimeId',
)({
  ssr: false,
  component: TagWorkspaceRuntimeSettingsRoute,
});

function TagWorkspaceRuntimeSettingsRoute() {
  const { workspaceSlug, machineId, runtimeId } = Route.useParams();
  return (
    <RuntimeWorkspaceRoute
      workspaceSlug={workspaceSlug}
      resetKeys={[machineId, runtimeId]}
    >
      <RuntimeSettingsPage machineId={machineId} runtimeId={runtimeId} />
    </RuntimeWorkspaceRoute>
  );
}
