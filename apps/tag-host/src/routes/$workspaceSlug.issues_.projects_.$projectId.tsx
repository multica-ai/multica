import { createFileRoute } from '@tanstack/react-router';
import { ProjectDetail } from '@multica/views/projects/components';
import { TaskWorkspaceRoute } from '@/workspace/task-workspace-route';

export const Route = createFileRoute(
  '/$workspaceSlug/issues_/projects_/$projectId'
)({
  ssr: false,
  component: TagWorkspaceProjectDetailRoute,
});

function TagWorkspaceProjectDetailRoute() {
  const { workspaceSlug, projectId } = Route.useParams();
  return (
    <TaskWorkspaceRoute workspaceSlug={workspaceSlug} resetKeys={[projectId]}>
      <ProjectDetail projectId={projectId} />
    </TaskWorkspaceRoute>
  );
}
