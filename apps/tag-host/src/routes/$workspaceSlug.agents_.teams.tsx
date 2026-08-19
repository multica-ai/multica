import { createFileRoute } from '@tanstack/react-router';
import { SquadsPage } from '@multica/views/squads';
import { AgentWorkspaceRoute } from '@/workspace/agent-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents_/teams')({
  ssr: false,
  component: TagWorkspaceTeamsRoute,
});

function TagWorkspaceTeamsRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <AgentWorkspaceRoute workspaceSlug={workspaceSlug}>
      <SquadsPage />
    </AgentWorkspaceRoute>
  );
}
