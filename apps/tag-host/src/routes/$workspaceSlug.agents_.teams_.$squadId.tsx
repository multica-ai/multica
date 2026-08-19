import { createFileRoute } from '@tanstack/react-router';
import { SquadDetailPage } from '@multica/views/squads';
import { AgentWorkspaceRoute } from '@/workspace/agent-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents_/teams_/$squadId')({
  ssr: false,
  component: TagWorkspaceTeamDetailRoute,
});

function TagWorkspaceTeamDetailRoute() {
  const { workspaceSlug, squadId } = Route.useParams();
  return (
    <AgentWorkspaceRoute workspaceSlug={workspaceSlug} resetKeys={[squadId]}>
      <SquadDetailPage
        squadId={squadId}
        collectionHref={`/${encodeURIComponent(workspaceSlug)}/agents/teams`}
        collectionLabel="Teams"
        createAgentHref={`/${encodeURIComponent(workspaceSlug)}/agents/new/manual?squad=${encodeURIComponent(squadId)}`}
      />
    </AgentWorkspaceRoute>
  );
}
