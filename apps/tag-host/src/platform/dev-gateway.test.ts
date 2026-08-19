// @vitest-environment node

import { describe, expect, it } from 'vitest';
import {
  resolveTagGatewayRequest,
  sanitizeTagProxyHeaders,
} from './dev-gateway';

describe('Tag Host unified local gateway', () => {
  it('keeps the browser on :3100 for Tag, API, WebSocket, upload and VIBES paths', () => {
    expect(resolveTagGatewayRequest('/tag/design-lab/chat')).toEqual({
      kind: 'tag-host',
      path: '/tag/design-lab/chat',
    });
    expect(resolveTagGatewayRequest('/api/tag/api/issues?limit=20')).toEqual({
      kind: 'multica-http',
      path: '/api/issues?limit=20',
    });
    expect(
      resolveTagGatewayRequest('/ws/tag?workspace_slug=design-lab')
    ).toEqual({
      kind: 'multica-websocket',
      path: '/ws?workspace_slug=design-lab',
    });
    expect(resolveTagGatewayRequest('/uploads/workspace/file.png')).toEqual({
      kind: 'multica-http',
      path: '/uploads/workspace/file.png',
    });
    expect(resolveTagGatewayRequest('/market')).toEqual({
      kind: 'vibes',
      path: '/market',
    });
  });

  it('serves the Tag manifest at its own scoped URL', () => {
    expect(resolveTagGatewayRequest('/tag/manifest.webmanifest')).toEqual({
      kind: 'tag-manifest',
      path: '/tag/manifest.webmanifest',
    });
  });

  it('keeps unported workspace surfaces in an explicit Tag migration state', () => {
    expect(resolveTagGatewayRequest('/tag/design-lab/inbox')).toEqual({
      kind: 'tag-migrating',
      path: '/tag/design-lab/inbox',
      workspaceSlug: 'design-lab',
      surface: 'inbox',
    });
    expect(
      resolveTagGatewayRequest('/tag/design-lab/members?tab=people')
    ).toEqual({
      kind: 'tag-migrating',
      path: '/tag/design-lab/members?tab=people',
      workspaceSlug: 'design-lab',
      surface: 'members',
    });
  });

  it('never forwards VIBES identity headers or unrelated cookies to Multica', () => {
    const source = {
      authorization: 'Bearer vibes-session',
      'proxy-authorization': 'Basic secret',
      cookie:
        'better-auth.session_token=vibes; multica_auth=tag; multica_csrf=csrf; last_workspace_slug=design-lab',
      'x-request-id': 'request-1',
    };

    expect(sanitizeTagProxyHeaders(source)).toEqual({
      cookie: 'multica_auth=tag; multica_csrf=csrf',
      'x-request-id': 'request-1',
    });
    expect(source.cookie).toContain('better-auth.session_token');
  });

  it('does not treat similar prefixes as Tag authority routes', () => {
    expect(resolveTagGatewayRequest('/api/tagged/api/issues')).toEqual({
      kind: 'vibes',
      path: '/api/tagged/api/issues',
    });
    expect(resolveTagGatewayRequest('/ws/tagged')).toEqual({
      kind: 'vibes',
      path: '/ws/tagged',
    });
    expect(resolveTagGatewayRequest('/uploads-legacy/file')).toEqual({
      kind: 'vibes',
      path: '/uploads-legacy/file',
    });
  });
});
