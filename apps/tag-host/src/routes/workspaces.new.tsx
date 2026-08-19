import { createFileRoute } from '@tanstack/react-router';
import { AuthorityCreateWorkspacePage } from '@multica/views/tag-authority';
import { TagHostProviders } from '@/platform/tag-host-providers';
import { useAuthorityWorkspaceSwitch } from '@/workspace/use-authority-workspace-switch';

export const Route = createFileRoute('/workspaces/new')({
  ssr: false,
  component: CreateWorkspaceRoute,
});

function CreateWorkspaceRoute() {
  return (
    <TagHostProviders>
      <CreateWorkspaceContent />
    </TagHostProviders>
  );
}

function CreateWorkspaceContent() {
  const { switchTo } = useAuthorityWorkspaceSwitch();
  return (
    <AuthorityCreateWorkspacePage
      onReady={(workspace) =>
        void switchTo(workspace, `/${encodeURIComponent(workspace.slug)}/chat`)
      }
    />
  );
}
