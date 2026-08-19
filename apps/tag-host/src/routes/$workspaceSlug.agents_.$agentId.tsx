import { createFileRoute } from '@tanstack/react-router';
import { AgentDetailPage } from '@multica/views/agents';
import { AgentWorkspaceRoute } from '@/workspace/agent-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents_/$agentId')({
  ssr: false,
  component: TagWorkspaceAgentDetailRoute,
});

function TagWorkspaceAgentDetailRoute() {
  const { workspaceSlug, agentId } = Route.useParams();
  return (
    <AgentWorkspaceRoute workspaceSlug={workspaceSlug} resetKeys={[agentId]}>
      <AgentDetailPage agentId={agentId} />
    </AgentWorkspaceRoute>
  );
}
