// @vitest-environment node

import { describe, expect, it } from 'vitest';
import { getRouter } from './router';

describe('Tag Host workspace routes', () => {
  it('exposes every approved collection and detail as a rendered root route', () => {
    const routes = Object.values(getRouter().routesById);
    const fullPaths = routes.map((route) => route.fullPath);

    expect(fullPaths).toEqual(
      expect.arrayContaining([
        '/$workspaceSlug/issues',
        '/$workspaceSlug/issues/$issueId',
        '/$workspaceSlug/issues/projects/$projectId',
        '/$workspaceSlug/issues/automations/$autopilotId',
        '/$workspaceSlug/agents',
        '/$workspaceSlug/agents/$agentId',
        '/$workspaceSlug/agents/new/manual',
        '/$workspaceSlug/agents/teams',
        '/$workspaceSlug/agents/teams/$squadId',
        '/$workspaceSlug/runtimes',
        '/$workspaceSlug/runtimes/$machineId',
        '/$workspaceSlug/runtimes/$machineId/runtime/$runtimeId',
        '/$workspaceSlug/members',
        '/$workspaceSlug/inbox',
        '/$workspaceSlug/settings',
        '/workspaces/new',
        '/invite',
        '/join',
      ])
    );
    expect(fullPaths).not.toContain('/onboarding');

    for (const fullPath of fullPaths.filter((path) => path !== '/')) {
      const route = routes.find((candidate) => candidate.fullPath === fullPath);
      expect(route?.parentRoute?.id, fullPath).toBe('__root__');
    }
  });

  it('does not resolve retired Agent Builder deep links', () => {
    const fullPaths = Object.values(getRouter().routesById).map(
      (route) => route.fullPath
    );

    expect(fullPaths).not.toContain('/$workspaceSlug/agents/new/ai');
    expect(fullPaths).not.toContain(
      '/$workspaceSlug/agents/new/ai/$sessionId'
    );
    expect(fullPaths).not.toContain('/$workspaceSlug/agents/new');
  });
});
