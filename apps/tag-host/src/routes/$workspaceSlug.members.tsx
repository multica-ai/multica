import { createFileRoute } from '@tanstack/react-router';
import { AuthorityMembersRoute } from '@/workspace/authority-members-route';

export const Route = createFileRoute('/$workspaceSlug/members')({
  ssr: false,
  component: TagWorkspaceMembersRoute,
});

function TagWorkspaceMembersRoute() {
  const { workspaceSlug } = Route.useParams();
  return <AuthorityMembersRoute workspaceSlug={workspaceSlug} />;
}
