import { createFileRoute } from '@tanstack/react-router';
import { TagWorkspaceSettings } from '@/workspace/tag-workspace-settings';

export const Route = createFileRoute('/$workspaceSlug/settings')({
  ssr: false,
  component: TagWorkspaceSettingsRoute,
});

function TagWorkspaceSettingsRoute() {
  const { workspaceSlug } = Route.useParams();
  return <TagWorkspaceSettings workspaceSlug={workspaceSlug} />;
}
