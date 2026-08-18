import { createFileRoute } from '@tanstack/react-router';
import { ChatPage } from '@multica/views/chat';
import { TagWorkspaceRoute } from '@/workspace/tag-workspace-route';

export const Route = createFileRoute('/$workspaceSlug/chat')({
  ssr: false,
  component: TagWorkspaceChatRoute,
});

function TagWorkspaceChatRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <TagWorkspaceRoute workspaceSlug={workspaceSlug}>
      <main className="flex h-full min-h-0 flex-col bg-background text-foreground">
        <ChatPage />
      </main>
    </TagWorkspaceRoute>
  );
}
