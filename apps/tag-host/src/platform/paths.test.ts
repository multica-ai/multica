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

  it('builds share links and explicit same-origin API and WebSocket bases', () => {
    expect(
      toTagShareUrl(
        'http://localhost:3000',
        '/design-lab/chat?session=session-1'
      )
    ).toBe('http://localhost:3000/tag/design-lab/chat?session=session-1');

    expect(resolveTagRuntimeUrls('https://vibes.test')).toEqual({
      apiBaseUrl: '/api/tag',
      wsUrl: 'wss://vibes.test/ws/tag',
    });
  });
});
