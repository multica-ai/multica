import { createFileRoute } from '@tanstack/react-router';
import { RuntimesPage } from '@multica/views/runtimes';
import { RuntimeWorkspaceRoute } from '@/workspace/runtime-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/runtimes')({
  ssr: false,
  component: TagWorkspaceRuntimesRoute,
});

function TagWorkspaceRuntimesRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <RuntimeWorkspaceRoute workspaceSlug={workspaceSlug}>
      <RuntimesPage />
    </RuntimeWorkspaceRoute>
  );
}
