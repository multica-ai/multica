import { createFileRoute } from '@tanstack/react-router';
import { ManualCreateAgentPage } from '@multica/views/agents';
import { TagWorkspaceRoute } from '@/workspace/tag-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/agents/new/manual')({
  ssr: false,
  component: TagWorkspaceManualAgentCreateRoute,
});

function TagWorkspaceManualAgentCreateRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <TagWorkspaceRoute workspaceSlug={workspaceSlug}>
      <main className="flex h-full min-h-0 flex-col bg-background text-foreground">
        <ManualCreateAgentPage />
      </main>
    </TagWorkspaceRoute>
  );
}
