import { createFileRoute } from '@tanstack/react-router';
import { SquadDetailPage } from '@multica/views/squads';
import { useT } from '@multica/views/i18n';
import { AgentWorkspaceRoute } from '@/workspace/agent-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents_/teams_/$squadId')({
  ssr: false,
  component: TagWorkspaceTeamDetailRoute,
});

function TagWorkspaceTeamDetailRoute() {
  const { workspaceSlug, squadId } = Route.useParams();
  const { t } = useT('squads');
  const { t: tIssues } = useT('issues');
  return (
    <AgentWorkspaceRoute workspaceSlug={workspaceSlug} resetKeys={[squadId]}>
      <SquadDetailPage
        squadId={squadId}
        collectionHref={`/${encodeURIComponent(workspaceSlug)}/agents/teams`}
        collectionLabel={t(($) => $.page.title)}
        createAgentHref={`/${encodeURIComponent(workspaceSlug)}/agents/new/manual?squad=${encodeURIComponent(squadId)}`}
        tasksHref={`/${encodeURIComponent(workspaceSlug)}/issues`}
        tasksLabel={tIssues(($) => $.task_center.tasks)}
      />
    </AgentWorkspaceRoute>
  );
}
