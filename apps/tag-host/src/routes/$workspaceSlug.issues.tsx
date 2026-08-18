import { createFileRoute } from '@tanstack/react-router';
import { TaskCenterPage } from '@multica/views/issues/components';
import { TaskCreateButton } from '@/workspace/task-create-button';
import { TaskWorkspaceRoute } from '@/workspace/task-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/issues')({
  ssr: false,
  component: TagWorkspaceTasksRoute,
});

function TagWorkspaceTasksRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <TaskWorkspaceRoute workspaceSlug={workspaceSlug}>
      <TaskCreateButton />
      <TaskCenterPage workspaceSlug={workspaceSlug} />
    </TaskWorkspaceRoute>
  );
}
