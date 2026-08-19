import { createFileRoute } from '@tanstack/react-router';
import { AuthorityInvitePage } from '@multica/views/tag-authority';
import { TagHostProviders } from '@/platform/tag-host-providers';
import { useAuthorityWorkspaceSwitch } from '@/workspace/use-authority-workspace-switch';

export const Route = createFileRoute('/invite')({
  ssr: false,
  validateSearch: (search: Record<string, unknown>) => ({
    token: typeof search.token === 'string' ? search.token : '',
  }),
  component: InviteRoute,
});

function InviteRoute() {
  return (
    <TagHostProviders>
      <InviteContent />
    </TagHostProviders>
  );
}

function InviteContent() {
  const { token } = Route.useSearch();
  const { switchTo } = useAuthorityWorkspaceSwitch();
  return (
    <AuthorityInvitePage
      token={token}
      onReady={(workspace) =>
        void switchTo(workspace, `/${encodeURIComponent(workspace.slug)}/chat`)
      }
    />
  );
}
