import { createHash } from 'node:crypto';
import { request as httpRequest, type IncomingMessage, type ServerResponse } from 'node:http';
import { connect, type Socket } from 'node:net';
import type { Plugin, ViteDevServer } from 'vite';
import {
  createTagManifest,
  createTagMigrationPage,
  resolveCanonicalTagRequest,
} from './canonical-entry';
import {
  authorizeTagGatewayForward,
  resolveTagGatewayRequest,
  type TagGatewayMintRequest,
  type TagGatewayMintResponse,
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

const TAG_GATEWAY_ASSERTION_PATH = '/api/tag-gateway/assertion';
const TAG_GATEWAY_MAX_BODY_BYTES = 101 << 20;
const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS', 'TRACE']);

class TagGatewayProxyError extends Error {
  constructor(readonly status: number) {
    super('Tag authority unavailable');
  }
}

function singleResponseHeader(
  headers: IncomingMessage['headers'],
  name: string
) {
  const value = headers[name];
  return typeof value === 'string' ? value : undefined;
}

export function mintTagGatewayAssertion(
  vibesOrigin: URL,
  cookie: string | undefined,
  input: TagGatewayMintRequest
) {
  const body = JSON.stringify(input);
  return new Promise<TagGatewayMintResponse | null>((resolve, reject) => {
    const request = httpRequest(
      new URL(TAG_GATEWAY_ASSERTION_PATH, vibesOrigin),
      {
        method: 'POST',
        headers: {
          'content-length': Buffer.byteLength(body).toString(),
          'content-type': 'application/json',
          origin: vibesOrigin.origin,
          'sec-fetch-site': 'same-origin',
          ...(cookie ? { cookie } : {}),
        },
      },
      (response) => {
        const status = response.statusCode ?? 503;
        const assertion = singleResponseHeader(
          response.headers,
          'x-vibes-tag-assertion'
        );
        const signature = singleResponseHeader(
          response.headers,
          'x-vibes-tag-assertion-signature'
        );
        const keyId = singleResponseHeader(
          response.headers,
          'x-vibes-tag-assertion-key-id'
        );
        response.resume();
        response.on('end', () => {
          if (status !== 204) {
            reject(
              new TagGatewayProxyError(
                status === 401 || status === 403 ? status : 503
              )
            );
            return;
          }
          resolve(
            assertion && signature && keyId
              ? { assertion, signature, keyId }
              : null
          );
        });
      }
    );
    request.on('error', () => reject(new TagGatewayProxyError(503)));
    request.end(body);
  });
}

function createTagGatewayMintClient(
  vibesOrigin: URL,
  cookie: string | undefined
) {
  return (input: TagGatewayMintRequest) =>
    mintTagGatewayAssertion(vibesOrigin, cookie, input);
}

async function readIncomingBody(incoming: IncomingMessage) {
  const chunks: Buffer[] = [];
  let length = 0;
  for await (const chunk of incoming) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    length += bytes.byteLength;
    if (length > TAG_GATEWAY_MAX_BODY_BYTES) {
      throw new TagGatewayProxyError(413);
    }
    chunks.push(bytes);
  }
  return Buffer.concat(chunks);
}

function proxyHttp(
  incoming: IncomingMessage,
  outgoing: ServerResponse,
  origin: URL,
  path: string,
  authorize: boolean,
  vibesOrigin: URL
) {
  if (!authorize) {
    const proxy = httpRequest(
      new URL(path, origin),
      { method: incoming.method, headers: incoming.headers },
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
    return;
  }

  void (async () => {
    try {
      const method = (incoming.method ?? 'GET').toUpperCase();
      const body = await readIncomingBody(incoming);
      if (SAFE_METHODS.has(method) && body.byteLength > 0) {
        throw new TagGatewayProxyError(400);
      }
      const headers = await authorizeTagGatewayForward({
        browserHeaders: incoming.headers,
        audience: 'vibes-tag-browser-http-v1',
        method,
        pathAndQuery: path,
        bodySha256: SAFE_METHODS.has(method)
          ? ''
          : createHash('sha256').update(body).digest('hex'),
        mint: createTagGatewayMintClient(
          vibesOrigin,
          incoming.headers.cookie
        ),
      });
      const proxy = httpRequest(
        new URL(path, origin),
        { method, headers },
        (response) => {
          outgoing.writeHead(response.statusCode ?? 502, response.headers);
          response.pipe(outgoing);
        }
      );
      proxy.on('error', () => {
        if (!outgoing.headersSent) outgoing.writeHead(502);
        outgoing.end('Local Tag gateway failed');
      });
      proxy.end(body);
    } catch (error) {
      const status =
        error instanceof TagGatewayProxyError ? error.status : 503;
      if (!outgoing.headersSent) outgoing.writeHead(status);
      outgoing.end('Tag authority unavailable');
    }
  })();
}

async function proxyWebSocket(
  request: IncomingMessage,
  browserSocket: Socket,
  head: Buffer,
  origin: URL,
  path: string,
  authorize: boolean,
  vibesOrigin: URL
) {
  let sourceHeaders = request.headers;
  if (authorize) {
    try {
      sourceHeaders = await authorizeTagGatewayForward({
        browserHeaders: request.headers,
        audience: 'vibes-tag-browser-ws-v1',
        method: 'GET',
        pathAndQuery: path,
        bodySha256: '',
        mint: createTagGatewayMintClient(
          vibesOrigin,
          request.headers.cookie
        ),
      });
    } catch (error) {
      const status =
        error instanceof TagGatewayProxyError ? error.status : 503;
      browserSocket.end(
        `HTTP/1.1 ${status} Tag authority unavailable\r\nConnection: close\r\n\r\n`
      );
      return;
    }
  }
  if (browserSocket.destroyed) return;
  const upstreamPort = Number(origin.port || 80);
  const upstream = connect(upstreamPort, origin.hostname, () => {
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

    void proxyWebSocket(
      request,
      socket,
      head,
      targetOrigin(target, vibesOrigin, apiOrigin),
      target.path,
      target.kind === 'multica-websocket',
      vibesOrigin
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
          target.kind === 'multica-http',
          vibesOrigin
        );
      });
    },
  };
}
