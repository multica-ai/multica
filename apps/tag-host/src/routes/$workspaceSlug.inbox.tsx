import { createFileRoute } from "@tanstack/react-router";
import { InboxPage } from "@multica/views/inbox";
import { TagWorkspaceRoute } from "@/workspace/tag-workspace-route";

export const Route = createFileRoute("/$workspaceSlug/inbox")({
  ssr: false,
  component: TagWorkspaceInboxRoute,
});

function TagWorkspaceInboxRoute() {
  const { workspaceSlug } = Route.useParams();
  return (
    <TagWorkspaceRoute workspaceSlug={workspaceSlug}>
      <InboxPage />
    </TagWorkspaceRoute>
  );
}
