// @vitest-environment jsdom

import { render, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { TagLaunchState } from './tag-launch-state';

describe('Tag launcher workspace state', () => {
  beforeEach(() => {
    document.cookie = 'last_workspace_slug=; path=/; max-age=0';
  });

  it('persists the authorized TanStack route workspace for refresh and PWA launch', async () => {
    render(<TagLaunchState workspaceSlug="design-lab" />);

    await waitFor(() => {
      expect(document.cookie).toContain('last_workspace_slug=design-lab');
    });
  });
});
