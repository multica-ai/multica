import { createFileRoute } from '@tanstack/react-router';
import { ChatPage } from '@multica/views/chat';
import { TagHostProviders } from '@/platform/tag-host-providers';
import { WorkspaceGate } from '@/workspace/workspace-gate';

export const Route = createFileRoute('/$workspaceSlug/chat')({
  ssr: false,
  component: TagWorkspaceChatRoute,
});

function TagWorkspaceChatRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <TagHostProviders>
      <WorkspaceGate workspaceSlug={workspaceSlug}>
        <ChatPage />
      </WorkspaceGate>
    </TagHostProviders>
  );
}
