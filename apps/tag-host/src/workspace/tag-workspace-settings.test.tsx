// @vitest-environment jsdom

import type { ReactNode } from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('@multica/views/settings', () => ({
  SettingsPage: ({
    hiddenWorkspaceTabs,
  }: {
    hiddenWorkspaceTabs: readonly string[];
  }) => (
    <output data-testid="hidden-settings-tabs">
      {hiddenWorkspaceTabs.join(',')}
    </output>
  ),
}));

vi.mock('./tag-workspace-route', () => ({
  TagWorkspaceRoute: ({
    workspaceSlug,
    children,
  }: {
    workspaceSlug: string;
    children: ReactNode;
  }) => <section data-workspace-slug={workspaceSlug}>{children}</section>,
}));

import { TagWorkspaceSettings } from './tag-workspace-settings';

describe('Tag Workspace Settings host wiring', () => {
  it('mounts the approved batch and excludes host-dependent tabs', () => {
    render(<TagWorkspaceSettings workspaceSlug="studio-a" />);

    const hiddenSettingsTabs = screen.getByTestId('hidden-settings-tabs');
    expect(hiddenSettingsTabs.textContent).toBe(
      'general,repositories,github,integrations,members,billing,plugins'
    );
    expect(
      hiddenSettingsTabs.closest('section')?.getAttribute('data-workspace-slug')
    ).toBe('studio-a');
  });
});
