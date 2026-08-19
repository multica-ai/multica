import { createFileRoute } from '@tanstack/react-router';
import { taskCenterPath } from '@multica/core/issues/task-center';
import { AutopilotDetailPage } from '@multica/views/autopilots/components';
import { useT } from '@multica/views/i18n';
import { TaskWorkspaceRoute } from '@/workspace/task-workspace-route';

export const Route = createFileRoute(
  '/$workspaceSlug/issues_/automations_/$autopilotId',
)({
  ssr: false,
  component: TagWorkspaceAutomationDetailRoute,
});

function TagWorkspaceAutomationDetailRoute() {
  const { workspaceSlug, autopilotId } = Route.useParams();
  const { t } = useT('issues');
  const collectionHref = taskCenterPath(workspaceSlug, 'automations');

  return (
    <TaskWorkspaceRoute
      workspaceSlug={workspaceSlug}
      resetKeys={[autopilotId]}
    >
      <AutopilotDetailPage
        autopilotId={autopilotId}
        collectionHref={collectionHref}
        collectionLabel={t(($) => $.task_center.automations)}
      />
    </TaskWorkspaceRoute>
  );
}
