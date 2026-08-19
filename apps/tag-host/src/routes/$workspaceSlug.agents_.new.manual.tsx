import { createFileRoute } from '@tanstack/react-router';
import { ManualCreateAgentPage } from '@multica/views/agents';
import { AgentWorkspaceRoute } from '@/workspace/agent-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents_/new/manual')({
  ssr: false,
  component: TagWorkspaceManualAgentCreateRoute,
});

function TagWorkspaceManualAgentCreateRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <AgentWorkspaceRoute workspaceSlug={workspaceSlug}>
      <ManualCreateAgentPage showWorkspaceSkills={false} />
    </AgentWorkspaceRoute>
  );
}
