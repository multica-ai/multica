import { SettingsPage } from '@multica/views/settings';
import { TagWorkspaceRoute } from './tag-workspace-route';

const SETTINGS_REQUIRING_HOST_ROUTES = [
  'general',
  'repositories',
  'github',
  'integrations',
  'members',
  'billing',
  'plugins',
] as const;

export function TagWorkspaceSettings({
  workspaceSlug,
}: {
  workspaceSlug: string;
}) {
  return (
    <TagWorkspaceRoute workspaceSlug={workspaceSlug}>
      <main className="flex h-full min-h-0 flex-col overflow-hidden">
        <SettingsPage hiddenWorkspaceTabs={SETTINGS_REQUIRING_HOST_ROUTES} />
      </main>
    </TagWorkspaceRoute>
  );
}
