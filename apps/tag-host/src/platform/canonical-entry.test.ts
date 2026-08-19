// @vitest-environment node

import { describe, expect, it } from 'vitest';
import {
  createTagManifest,
  createTagMigrationPage,
  resolveCanonicalTagRequest,
} from './canonical-entry';

describe('TanStack Tag canonical browser entry', () => {
  it('launches /tag into the last authorized workspace without a Next hop', () => {
    expect(
      resolveCanonicalTagRequest({
        pathname: '/tag',
        search: '',
        cookie: 'multica_auth=session; last_workspace_slug=design-lab',
      })
    ).toEqual({ redirect: '/tag/design-lab/chat' });

    expect(
      resolveCanonicalTagRequest({
        pathname: '/tag',
        search: '',
        cookie:
          'better-auth.session_token=vibes; last_workspace_slug=design-lab',
      })
    ).toEqual({ redirect: '/tag-entry?workspace=design-lab' });
  });

  it('sends an explicitly requested workspace through the VIBES-owned handoff', () => {
    expect(
      resolveCanonicalTagRequest({
        pathname: '/tag',
        search: '?workspace=research-lab&page=%2Fissues%3Ftab%3Dprojects',
        cookie: 'last_workspace_slug=design-lab',
      })
    ).toEqual({
      redirect:
        '/tag-entry?workspace=research-lab&page=%2Fissues%3Ftab%3Dprojects',
    });
  });

  it('redirects old Next-host core links to canonical TanStack paths', () => {
    const cookie = 'last_workspace_slug=design-lab';

    expect(
      resolveCanonicalTagRequest({
        pathname: '/issues/task-1',
        search: '?view=activity',
        cookie,
      })
    ).toEqual({
      redirect: '/tag/design-lab/issues/task-1?view=activity',
    });
    expect(
      resolveCanonicalTagRequest({
        pathname: '/design-lab/projects/project-1',
        search: '',
        cookie,
      })
    ).toEqual({
      redirect: '/tag/design-lab/issues/projects/project-1',
    });
    expect(
      resolveCanonicalTagRequest({
        pathname: '/tag/design-lab/squads/team-1',
        search: '',
        cookie,
      })
    ).toEqual({
      redirect: '/tag/design-lab/agents/teams/team-1',
    });
    expect(
      resolveCanonicalTagRequest({
        pathname: '/design-lab/inbox',
        search: '',
        cookie,
      })
    ).toEqual({ redirect: '/tag/design-lab/inbox' });
  });

  it('leaves VIBES and later-ticket surfaces outside the compatibility redirect', () => {
    for (const pathname of [
      '/',
      '/settings',
      '/messages',
      '/inbox',
      '/auth/callback',
    ]) {
      expect(
        resolveCanonicalTagRequest({
          pathname,
          search: '',
          cookie: 'last_workspace_slug=design-lab',
        })
      ).toBeNull();
    }
  });

  it('renders an explicit Tag-owned placeholder for unported workspace surfaces', () => {
    expect(
      createTagMigrationPage({
        workspaceSlug: 'design-lab',
        surface: 'inbox',
      })
    ).toContain('Migrating to Tag');
  });

  it('leaves malformed legacy paths untouched instead of crashing the gateway', () => {
    expect(
      resolveCanonicalTagRequest({
        pathname: '/tag/%E0%A4%A/issues',
        search: '',
        cookie: 'last_workspace_slug=design-lab',
      })
    ).toBeNull();
  });

  it('publishes a Tag-scoped launcher instead of reusing the Next manifest', () => {
    expect(createTagManifest()).toMatchObject({
      id: '/tag/',
      name: 'VIBES Tag',
      short_name: 'Tag',
      start_url: '/tag/',
      scope: '/tag/',
      display: 'standalone',
    });
  });
});
