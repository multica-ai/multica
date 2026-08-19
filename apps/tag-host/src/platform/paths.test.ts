// @vitest-environment node

import { describe, expect, it } from 'vitest';
import {
  fromTagHostLocation,
  resolveTagRuntimeUrls,
  toTagHostPath,
  toTagShareUrl,
} from './paths';

describe('TanStack Tag Host path adapter', () => {
  it('keeps Multica workspace paths behind the /tag host prefix', () => {
    expect(toTagHostPath('/design-lab/chat?session=session-1')).toBe(
      '/tag/design-lab/chat?session=session-1'
    );
    expect(toTagHostPath('/tag/design-lab/chat')).toBe(
      '/tag/design-lab/chat'
    );
  });

  it('exposes Multica pathname and search state without the host prefix', () => {
    const location = fromTagHostLocation(
      '/tag/design-lab/chat',
      '?session=session-1&panel=activity'
    );

    expect(location.pathname).toBe('/design-lab/chat');
    expect(location.searchParams.get('session')).toBe('session-1');
    expect(location.searchParams.get('panel')).toBe('activity');
  });

  it('mounts canonical Project links beneath Tasks and restores them after refresh', () => {
    expect(toTagHostPath('/design-lab/projects')).toBe(
      '/tag/design-lab/issues?tab=projects'
    );
    expect(
      toTagHostPath('/design-lab/projects/project%201?view=table#tasks')
    ).toBe('/tag/design-lab/issues/projects/project%201?view=table#tasks');

    const list = fromTagHostLocation(
      '/tag/design-lab/issues',
      '?tab=projects'
    );
    expect(list.pathname).toBe('/design-lab/projects');
    expect(list.searchParams.get('tab')).toBe('projects');

    const detail = fromTagHostLocation(
      '/tag/design-lab/issues/projects/project-1',
      ''
    );
    expect(detail.pathname).toBe('/design-lab/projects/project-1');
  });

  it('builds share links and explicit same-origin API and WebSocket bases', () => {
    expect(
      toTagShareUrl(
        'http://localhost:3000',
        '/design-lab/chat?session=session-1'
      )
    ).toBe('http://localhost:3000/tag/design-lab/chat?session=session-1');
    expect(
      toTagShareUrl('http://localhost:3000', '/design-lab/projects/project-1')
    ).toBe('http://localhost:3000/tag/design-lab/issues/projects/project-1');
    expect(
      toTagShareUrl('http://localhost:3100', '/design-lab/inbox?issue=task-1')
    ).toBe('http://localhost:3100/tag/design-lab/inbox?issue=task-1');

    const inbox = fromTagHostLocation(
      '/tag/design-lab/inbox',
      '?issue=task-1'
    );
    expect(inbox.pathname).toBe('/design-lab/inbox');
    expect(inbox.searchParams.get('issue')).toBe('task-1');

    expect(resolveTagRuntimeUrls('https://vibes.test')).toEqual({
      apiBaseUrl: '/api/tag',
      wsUrl: 'wss://vibes.test/ws/tag',
    });
  });
});
