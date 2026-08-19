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

    for (const fullPath of fullPaths.filter((path) => path !== '/')) {
      const route = routes.find((candidate) => candidate.fullPath === fullPath);
      expect(route?.parentRoute?.id, fullPath).toBe('__root__');
    }
  });
});
