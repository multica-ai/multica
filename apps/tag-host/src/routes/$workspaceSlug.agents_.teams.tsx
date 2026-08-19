import { createFileRoute } from '@tanstack/react-router';
import { SquadsPage } from '@multica/views/squads';
import { useT } from '@multica/views/i18n';
import { AgentWorkspaceRoute } from '@/workspace/agent-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents_/teams')({
  ssr: false,
  component: TagWorkspaceTeamsRoute,
});

function TagWorkspaceTeamsRoute() {
  const { workspaceSlug } = Route.useParams();
  const { t } = useT('squads');
  return (
    <AgentWorkspaceRoute workspaceSlug={workspaceSlug}>
      <SquadsPage
        title={t(($) => $.page.title)}
        detailHref={(squadId) =>
          `/${encodeURIComponent(workspaceSlug)}/agents/teams/${encodeURIComponent(squadId)}`
        }
      />
    </AgentWorkspaceRoute>
  );
}
