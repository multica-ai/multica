// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const state = vi.hoisted(() => ({
  user: { id: 'user-1' } as { id: string } | null,
  isLoading: false,
  currentSlug: null as string | null,
  currentWorkspaceId: null as string | null,
  workspaceFailure: false,
}));

vi.mock('@multica/core/auth', () => ({
  useAuthStore: (selector: (value: typeof state) => unknown) => selector(state),
}));

vi.mock('@multica/core/paths', () => ({
  WorkspaceSlugProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

vi.mock('@multica/core/platform', () => ({
  getCurrentSlug: () => state.currentSlug,
  setCurrentWorkspace: (slug: string | null, workspaceId: string | null) => {
    state.currentSlug = slug;
    state.currentWorkspaceId = workspaceId;
  },
}));

vi.mock('@multica/core/workspace', () => ({
  workspaceBySlugOptions: (slug: string) => ({
    queryKey: ['workspace-by-slug', slug],
    queryFn: async () => {
      if (state.workspaceFailure) throw new Error('workspace service offline');
      return null;
    },
  }),
}));

import { WorkspaceGate } from './workspace-gate';

function ChildProbe() {
  return <div data-testid="child-workspace">{state.currentSlug ?? 'none'}</div>;
}

function renderGate(queryClient: QueryClient) {
  return render(
    <QueryClientProvider client={queryClient}>
      <WorkspaceGate workspaceSlug="design-lab">
        <ChildProbe />
      </WorkspaceGate>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  state.user = { id: 'user-1' };
  state.isLoading = false;
  state.currentSlug = null;
  state.currentWorkspaceId = null;
  state.workspaceFailure = false;
});

afterEach(cleanup);

describe('WorkspaceGate', () => {
  it('does not mount child queries before the workspace resolves', () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    renderGate(queryClient);

    expect(screen.queryByTestId('child-workspace')).toBeNull();
    expect(screen.getByText('Resolving workspace')).toBeTruthy();
    expect(state.currentSlug).toBeNull();
  });

  it('clears a stale workspace before a different slug resolves', () => {
    state.currentSlug = 'previous-workspace';
    state.currentWorkspaceId = 'workspace-previous';
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    renderGate(queryClient);

    expect(screen.queryByTestId('child-workspace')).toBeNull();
    expect(state.currentSlug).toBeNull();
    expect(state.currentWorkspaceId).toBeNull();
  });

  it('sets the correct workspace synchronously before mounting children', () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(['workspace-by-slug', 'design-lab'], {
      id: 'workspace-1',
      slug: 'design-lab',
    });

    renderGate(queryClient);

    expect(screen.getByTestId('child-workspace').textContent).toBe(
      'design-lab'
    );
    expect(state.currentWorkspaceId).toBe('workspace-1');
  });

  it('shows an explicit retry state when Workspace resolution fails', async () => {
    state.workspaceFailure = true;
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    renderGate(queryClient);

    expect(await screen.findByText('Could not load workspace')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
    expect(screen.queryByTestId('child-workspace')).toBeNull();
  });

  it('leaves a 44px return to VIBES action when the session is unavailable', () => {
    state.user = null;
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    renderGate(queryClient);

    const returnLink = screen.getByRole('link', { name: 'Return to VIBES' });
    expect(returnLink.getAttribute('href')).toBe('/');
    expect(returnLink.className).toContain('min-h-11');
    expect(returnLink.className).toContain('min-w-11');
  });

  it('names the no-access possibility without exposing workspace existence', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    renderGate(queryClient);

    expect(
      await screen.findByText('Workspace unavailable or access denied')
    ).toBeTruthy();
    expect(screen.queryByTestId('child-workspace')).toBeNull();
  });
});
