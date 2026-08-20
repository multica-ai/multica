// @vitest-environment node

import { createServer, type IncomingHttpHeaders } from 'node:http';
import type { AddressInfo } from 'node:net';
import { describe, expect, it, vi } from 'vitest';
import { mintTagGatewayAssertion } from './dev-gateway-plugin';
import {
  authorizeTagGatewayForward,
  resolveTagGatewayRequest,
  sanitizeTagProxyHeaders,
  type TagGatewayMintRequest,
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

  it('drops every browser identity header and cookie before Multica forwarding', () => {
    const source = {
      authorization: 'Bearer vibes-session',
      'proxy-authorization': 'Basic secret',
      cookie:
        'better-auth.session_token=vibes; multica_auth=tag; multica_csrf=csrf; last_workspace_slug=design-lab',
      'x-vibes-tag-assertion': 'forged',
      'x-tag-user-id': 'forged-user',
      'x-internal-user-id': 'forged-internal-user',
      'x-request-id': 'request-1',
    };

    expect(sanitizeTagProxyHeaders(source)).toEqual({
      'x-request-id': 'request-1',
    });
    expect(source.cookie).toContain('better-auth.session_token');
  });

  it('mints through the same-origin VIBES mutation boundary', async () => {
    let receivedHeaders: IncomingHttpHeaders = {};
    const server = createServer((request, response) => {
      receivedHeaders = request.headers;
      request.resume();
      request.on('end', () => {
        response.writeHead(204, {
          'x-vibes-tag-assertion': 'payload',
          'x-vibes-tag-assertion-signature': 'signature',
          'x-vibes-tag-assertion-key-id': 'gateway-key',
        });
        response.end();
      });
    });
    await new Promise<void>((resolve) =>
      server.listen(0, '127.0.0.1', resolve)
    );

    try {
      const address = server.address() as AddressInfo;
      const origin = new URL(`http://127.0.0.1:${address.port}`);
      await expect(
        mintTagGatewayAssertion(origin, 'better-auth.session_token=vibes', {
          audience: 'vibes-tag-browser-http-v1',
          method: 'GET',
          pathAndQuery: '/api/workspaces',
          workspaceSlug: 'design-lab',
          bodySha256: '',
        })
      ).resolves.toEqual({
        assertion: 'payload',
        signature: 'signature',
        keyId: 'gateway-key',
      });
      expect(receivedHeaders.origin).toBe(origin.origin);
      expect(receivedHeaders['sec-fetch-site']).toBe('same-origin');
      expect(receivedHeaders.cookie).toBe('better-auth.session_token=vibes');
    } finally {
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve()))
      );
    }
  });

  it('times out when VIBES accepts the mint connection but never responds', async () => {
    const server = createServer(() => {});
    await new Promise<void>((resolve) =>
      server.listen(0, '127.0.0.1', resolve)
    );

    try {
      const address = server.address() as AddressInfo;
      await expect(
        mintTagGatewayAssertion(
          new URL(`http://127.0.0.1:${address.port}`),
          'better-auth.session_token=vibes',
          {
            audience: 'vibes-tag-browser-http-v1',
            method: 'GET',
            pathAndQuery: '/api/workspaces',
            workspaceSlug: 'design-lab',
            bodySha256: '',
          },
          20
        )
      ).rejects.toThrow('Tag authority unavailable');
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve()))
      );
    }
  });

  it('mints fresh authority and injects all three assertion headers for every HTTP forward', async () => {
    const calls: TagGatewayMintRequest[] = [];
    const source = {
      cookie: 'better-auth.session_token=vibes; multica_auth=stale',
      'x-workspace-slug': 'design-lab',
      'x-vibes-tag-assertion': 'forged',
      'x-request-id': 'request-1',
    };
    const mint = async (input: TagGatewayMintRequest) => {
      calls.push(input);
      return {
        assertion: 'payload',
        signature: 'signature',
        keyId: 'gateway-key',
      };
    };
    const input = {
      browserHeaders: source,
      audience: 'vibes-tag-browser-http-v1' as const,
      method: 'POST',
      pathAndQuery: '/api/issues?limit=20',
      bodySha256: 'a'.repeat(64),
      mint,
    };

    const first = await authorizeTagGatewayForward(input);
    const second = await authorizeTagGatewayForward(input);

    expect(calls).toEqual([
      {
        audience: 'vibes-tag-browser-http-v1',
        method: 'POST',
        pathAndQuery: '/api/issues?limit=20',
        workspaceSlug: 'design-lab',
        bodySha256: 'a'.repeat(64),
      },
      {
        audience: 'vibes-tag-browser-http-v1',
        method: 'POST',
        pathAndQuery: '/api/issues?limit=20',
        workspaceSlug: 'design-lab',
        bodySha256: 'a'.repeat(64),
      },
    ]);
    for (const headers of [first, second]) {
      expect(headers.cookie).toBeUndefined();
      expect(headers['x-vibes-tag-assertion']).toBe('payload');
      expect(headers['x-vibes-tag-assertion-signature']).toBe('signature');
      expect(headers['x-vibes-tag-assertion-key-id']).toBe('gateway-key');
    }
  });

  it('uses the WebSocket target workspace and fails closed without fresh authority', async () => {
    const mint = vi.fn(async () => ({
      assertion: 'ws-payload',
      signature: 'ws-signature',
      keyId: 'gateway-key',
    }));
    await authorizeTagGatewayForward({
      browserHeaders: { cookie: 'better-auth.session_token=vibes' },
      audience: 'vibes-tag-browser-ws-v1',
      method: 'GET',
      pathAndQuery: '/ws?workspace_slug=design-lab',
      bodySha256: '',
      mint,
    });
    expect(mint).toHaveBeenCalledWith({
      audience: 'vibes-tag-browser-ws-v1',
      method: 'GET',
      pathAndQuery: '/ws?workspace_slug=design-lab',
      workspaceSlug: 'design-lab',
      bodySha256: '',
    });

    await expect(
      authorizeTagGatewayForward({
        browserHeaders: { 'x-workspace-slug': 'design-lab' },
        audience: 'vibes-tag-browser-http-v1',
        method: 'GET',
        pathAndQuery: '/api/issues',
        bodySha256: '',
        async mint() {
          return null;
        },
      })
    ).rejects.toThrow('Tag authority unavailable');
  });

  it('uses the canonical Tag referrer to scope bootstrap API requests', async () => {
    const mint = vi.fn(async () => ({
      assertion: 'payload',
      signature: 'signature',
      keyId: 'gateway-key',
    }));

    await authorizeTagGatewayForward({
      browserHeaders: {
        referer: 'http://localhost:3100/tag/design-lab/chat',
      },
      audience: 'vibes-tag-browser-http-v1',
      method: 'GET',
      pathAndQuery: '/api/auth/me',
      bodySha256: '',
      mint,
    });

    expect(mint).toHaveBeenCalledWith({
      audience: 'vibes-tag-browser-http-v1',
      method: 'GET',
      pathAndQuery: '/api/auth/me',
      workspaceSlug: 'design-lab',
      bodySha256: '',
    });
  });

  it('uses the VIBES handoff referrer to scope the initial exchange', async () => {
    const mint = vi.fn(async () => ({
      assertion: 'payload',
      signature: 'signature',
      keyId: 'gateway-key',
    }));

    await authorizeTagGatewayForward({
      browserHeaders: {
        referer:
          'http://localhost:3100/tag-entry?workspace=design-lab&page=%2Fchat',
      },
      audience: 'vibes-tag-browser-http-v1',
      method: 'POST',
      pathAndQuery: '/api/auth/vibes-handoff',
      bodySha256: 'a'.repeat(64),
      mint,
    });

    expect(mint).toHaveBeenCalledWith({
      audience: 'vibes-tag-browser-http-v1',
      method: 'POST',
      pathAndQuery: '/api/auth/vibes-handoff',
      workspaceSlug: 'design-lab',
      bodySha256: 'a'.repeat(64),
    });
  });

  it('fails closed when Tag workspace scope sources disagree', async () => {
    await expect(
      authorizeTagGatewayForward({
        browserHeaders: {
          referer: 'http://localhost:3100/tag/design-lab/chat',
          'x-workspace-slug': 'other-workspace',
        },
        audience: 'vibes-tag-browser-http-v1',
        method: 'GET',
        pathAndQuery: '/api/auth/me',
        bodySha256: '',
        async mint() {
          return {
            assertion: 'payload',
            signature: 'signature',
            keyId: 'gateway-key',
          };
        },
      })
    ).rejects.toThrow('Tag workspace scope mismatch');
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
