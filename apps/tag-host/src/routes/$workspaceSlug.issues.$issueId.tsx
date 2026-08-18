import { createFileRoute } from '@tanstack/react-router';
import { IssueDetailRoute } from '@multica/views/issues/components';
import { TaskWorkspaceRoute } from '@/workspace/task-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/issues/$issueId')({
  ssr: false,
  component: TagWorkspaceTaskDetailRoute,
});

function TagWorkspaceTaskDetailRoute() {
  const { workspaceSlug, issueId } = Route.useParams();
  return (
    <TaskWorkspaceRoute workspaceSlug={workspaceSlug} resetKeys={[issueId]}>
      <IssueDetailRoute routeId={issueId} />
    </TaskWorkspaceRoute>
  );
}
