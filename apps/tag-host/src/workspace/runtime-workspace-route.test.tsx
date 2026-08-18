// @vitest-environment jsdom

import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

const observed = vi.hoisted(() => ({
  workspaceSlug: "",
  resetKeys: undefined as unknown[] | undefined,
}));

vi.mock('./tag-workspace-route', () => ({
  TagWorkspaceRoute: ({
    workspaceSlug,
    resetKeys,
    children,
  }: {
    workspaceSlug: string;
    resetKeys?: unknown[];
    children: React.ReactNode;
  }) => {
    observed.workspaceSlug = workspaceSlug;
    observed.resetKeys = resetKeys;
    return <div data-testid="workspace-route">{children}</div>;
  },
}));

import { RuntimeWorkspaceRoute } from './runtime-workspace-route';

describe('RuntimeWorkspaceRoute', () => {
  it('keeps Runtime views inside the workspace shell and resets page failures by deep-link id', () => {
    render(
      <RuntimeWorkspaceRoute workspaceSlug="studio-a" resetKeys={["machine-a"]}>
        <div>Runtime content</div>
      </RuntimeWorkspaceRoute>,
    );

    expect(observed.workspaceSlug).toBe('studio-a');
    expect(observed.resetKeys).toEqual(['machine-a']);
    expect(screen.getByTestId('workspace-route').textContent).toContain(
      'Runtime content',
    );
    expect(screen.getByRole("main").className).toContain("min-w-0");
  });
});
