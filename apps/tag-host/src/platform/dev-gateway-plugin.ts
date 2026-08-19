import { request as httpRequest, type IncomingMessage, type ServerResponse } from 'node:http';
import { connect, type Socket } from 'node:net';
import type { Plugin, ViteDevServer } from 'vite';
import {
  createTagManifest,
  createTagMigrationPage,
  resolveCanonicalTagRequest,
} from './canonical-entry';
import {
  resolveTagGatewayRequest,
  sanitizeTagProxyHeaders,
  type TagGatewayTarget,
} from './dev-gateway';

const LOOPBACK_HOSTS = new Set(['127.0.0.1', 'localhost', '[::1]']);

function localOrigin(name: string, value: string) {
  const origin = new URL(value);
  if (origin.protocol !== 'http:' || !LOOPBACK_HOSTS.has(origin.hostname)) {
    throw new Error(`${name} must use a local HTTP loopback origin`);
  }
  return origin;
}

function proxyHttp(
  incoming: IncomingMessage,
  outgoing: ServerResponse,
  origin: URL,
  path: string,
  sanitize: boolean
) {
  const proxy = httpRequest(
    new URL(path, origin),
    {
      method: incoming.method,
      headers: sanitize
        ? sanitizeTagProxyHeaders(incoming.headers)
        : incoming.headers,
    },
    (response) => {
      outgoing.writeHead(response.statusCode ?? 502, response.headers);
      response.pipe(outgoing);
    }
  );
  proxy.on('error', (error) => {
    if (!outgoing.headersSent) outgoing.writeHead(502);
    outgoing.end(`Local Tag gateway failed: ${error.message}`);
  });
  incoming.pipe(proxy);
}

function proxyWebSocket(
  request: IncomingMessage,
  browserSocket: Socket,
  head: Buffer,
  origin: URL,
  path: string,
  sanitize: boolean
) {
  const upstreamPort = Number(origin.port || 80);
  const upstream = connect(upstreamPort, origin.hostname, () => {
    const sourceHeaders = sanitize
      ? sanitizeTagProxyHeaders(request.headers)
      : request.headers;
    const headers = Object.entries(sourceHeaders)
      .filter(([, value]) => value !== undefined)
      .map(([name, value]) => `${name}: ${String(value)}`)
      .join('\r\n');
    upstream.write(
      `${request.method ?? 'GET'} ${path} HTTP/${request.httpVersion}\r\n${headers}\r\n\r\n`
    );
    if (head.length > 0) upstream.write(head);
    browserSocket.pipe(upstream).pipe(browserSocket);
  });
  upstream.on('error', () => browserSocket.destroy());
  browserSocket.on('error', () => upstream.destroy());
}

function targetOrigin(
  target: TagGatewayTarget,
  vibesOrigin: URL,
  apiOrigin: URL
) {
  return target.kind === 'vibes' ? vibesOrigin : apiOrigin;
}

function installUpgradeProxy(
  server: ViteDevServer,
  vibesOrigin: URL,
  apiOrigin: URL
) {
  server.httpServer?.on('upgrade', (request, socket, head) => {
    const path = request.url ?? '/';
    const target = resolveTagGatewayRequest(path);
    if (target.kind === 'tag-host') return;
    if (
      target.kind === 'tag-manifest' ||
      target.kind === 'tag-migrating' ||
      target.kind === 'multica-http'
    ) {
      socket.end('HTTP/1.1 404 Not Found\r\n\r\n');
      return;
    }

    proxyWebSocket(
      request,
      socket,
      head,
      targetOrigin(target, vibesOrigin, apiOrigin),
      target.path,
      target.kind === 'multica-websocket'
    );
  });
}

export function vibesTagUnifiedGateway(): Plugin {
  return {
    name: 'vibes-tag-unified-gateway',
    configureServer(server) {
      const vibesOrigin = localOrigin(
        'VIBES origin',
        process.env.VCC_TAG_VIBES_ORIGIN ?? 'http://127.0.0.1:3101'
      );
      const apiOrigin = localOrigin(
        'Multica API origin',
        process.env.VCC_TAG_API_ORIGIN ?? 'http://127.0.0.1:8080'
      );
      installUpgradeProxy(server, vibesOrigin, apiOrigin);

      server.middlewares.use((incoming, outgoing, next) => {
        const requestUrl = new URL(
          incoming.url ?? '/',
          'http://localhost:3100'
        );
        const canonical = resolveCanonicalTagRequest({
          pathname: requestUrl.pathname,
          search: requestUrl.search,
          cookie: incoming.headers.cookie,
        });
        if (canonical) {
          outgoing.writeHead(307, {
            'cache-control': 'no-store',
            location: canonical.redirect,
          });
          outgoing.end();
          return;
        }

        const target = resolveTagGatewayRequest(
          `${requestUrl.pathname}${requestUrl.search}`
        );
        if (target.kind === 'tag-host') {
          next();
          return;
        }
        if (target.kind === 'tag-manifest') {
          outgoing.writeHead(200, {
            'cache-control': 'no-store',
            'content-type': 'application/manifest+json; charset=utf-8',
          });
          outgoing.end(JSON.stringify(createTagManifest()));
          return;
        }
        if (target.kind === 'tag-migrating') {
          outgoing.writeHead(200, {
            'cache-control': 'no-store',
            'content-type': 'text/html; charset=utf-8',
          });
          outgoing.end(createTagMigrationPage(target));
          return;
        }
        if (target.kind === 'multica-websocket') {
          outgoing.writeHead(426, { 'content-type': 'text/plain; charset=utf-8' });
          outgoing.end('WebSocket upgrade required');
          return;
        }

        proxyHttp(
          incoming,
          outgoing,
          targetOrigin(target, vibesOrigin, apiOrigin),
          target.path,
          target.kind === 'multica-http'
        );
      });
    },
  };
}
