import { createFileRoute } from '@tanstack/react-router';
import { AgentDetailPage } from '@multica/views/agents';
import { TagWorkspaceRoute } from '@/workspace/tag-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents/$agentId')({
  ssr: false,
  component: TagWorkspaceAgentDetailRoute,
});

function TagWorkspaceAgentDetailRoute() {
  const { workspaceSlug, agentId } = Route.useParams();
  return (
    <TagWorkspaceRoute workspaceSlug={workspaceSlug} resetKeys={[agentId]}>
      <main className="flex h-full min-h-0 flex-col bg-background text-foreground">
        <AgentDetailPage agentId={agentId} />
      </main>
    </TagWorkspaceRoute>
  );
}
