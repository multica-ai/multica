import { createFileRoute } from '@tanstack/react-router';
import { AgentsPage } from '@multica/views/agents';
import { TagWorkspaceRoute } from '@/workspace/tag-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents')({
  ssr: false,
  component: TagWorkspaceAgentsRoute,
});

function TagWorkspaceAgentsRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <TagWorkspaceRoute workspaceSlug={workspaceSlug}>
      <main className="flex h-full min-h-0 flex-col bg-background text-foreground">
        <AgentsPage />
      </main>
    </TagWorkspaceRoute>
  );
}
