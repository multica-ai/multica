import { createFileRoute } from '@tanstack/react-router';
import { WorkspaceFilesPage } from '@multica/views/attachments';
import { ChatWorkspaceRoute } from '@/workspace/chat-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/chat_/files')({
  ssr: false,
  component: TagWorkspaceFilesRoute,
});

function TagWorkspaceFilesRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <ChatWorkspaceRoute workspaceSlug={workspaceSlug}>
      <WorkspaceFilesPage />
    </ChatWorkspaceRoute>
  );
}
