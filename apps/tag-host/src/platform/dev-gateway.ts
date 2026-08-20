import { safeWorkspaceSlug } from './canonical-entry';

export type TagGatewayTarget =
  | { readonly kind: 'tag-host'; readonly path: string }
  | { readonly kind: 'tag-manifest'; readonly path: string }
  | {
      readonly kind: 'tag-migrating';
      readonly path: string;
      readonly workspaceSlug: string;
      readonly surface: 'inbox' | 'members';
    }
  | { readonly kind: 'multica-http'; readonly path: string }
  | { readonly kind: 'multica-websocket'; readonly path: string }
  | { readonly kind: 'vibes'; readonly path: string };

export type ProxyHeaders = Record<string, string | string[] | undefined>;

export type TagGatewayAudience =
  | 'vibes-tag-browser-http-v1'
  | 'vibes-tag-browser-ws-v1';

export interface TagGatewayMintRequest {
  readonly audience: TagGatewayAudience;
  readonly method: string;
  readonly pathAndQuery: string;
  readonly workspaceSlug: string;
  readonly bodySha256: string;
}

export interface TagGatewayMintResponse {
  readonly assertion: string;
  readonly signature: string;
  readonly keyId: string;
}

const PRIVATE_HEADER_PREFIXES = ['x-vibes-', 'x-tag-', 'x-internal-'];
const PRIVATE_HEADERS = new Set([
  'authorization',
  'cookie',
  'proxy-authorization',
]);
function stripExactPrefix(path: string, prefix: string) {
  if (path === prefix) return '/';
  if (!path.startsWith(`${prefix}/`) && !path.startsWith(`${prefix}?`)) {
    return null;
  }
  return path.slice(prefix.length) || '/';
}

export function resolveTagGatewayRequest(path: string): TagGatewayTarget {
  if (path === '/tag/manifest.webmanifest') {
    return { kind: 'tag-manifest', path };
  }

  const tagApiPath = stripExactPrefix(path, '/api/tag');
  if (tagApiPath) return { kind: 'multica-http', path: tagApiPath };

  const tagWebSocketPath = stripExactPrefix(path, '/ws/tag');
  if (tagWebSocketPath) {
    return {
      kind: 'multica-websocket',
      path:
        tagWebSocketPath === '/'
          ? '/ws'
          : `/ws${tagWebSocketPath}`,
    };
  }

  if (path === '/uploads' || path.startsWith('/uploads/')) {
    return { kind: 'multica-http', path };
  }

  const migrationMatch = path.match(
    /^\/tag\/([a-z0-9][a-z0-9-]{0,127})\/(inbox|members)(?=\/|\?|$)/u
  );
  if (migrationMatch) {
    return {
      kind: 'tag-migrating',
      path,
      workspaceSlug: migrationMatch[1] ?? '',
      surface: migrationMatch[2] as 'inbox' | 'members',
    };
  }

  if (path === '/tag' || path.startsWith('/tag/')) {
    return { kind: 'tag-host', path };
  }

  return { kind: 'vibes', path };
}

export function sanitizeTagProxyHeaders(headers: ProxyHeaders): ProxyHeaders {
  const sanitized: ProxyHeaders = {};
  for (const [name, value] of Object.entries(headers)) {
    const lower = name.toLowerCase();
    if (
      PRIVATE_HEADERS.has(lower) ||
      PRIVATE_HEADER_PREFIXES.some((prefix) => lower.startsWith(prefix))
    ) {
      continue;
    }
    sanitized[name] = value;
  }
  return sanitized;
}

function singleHeader(headers: ProxyHeaders, name: string) {
  for (const [candidate, value] of Object.entries(headers)) {
    if (candidate.toLowerCase() !== name) continue;
    return Array.isArray(value) ? value[0] : value;
  }
  return undefined;
}

function resolveWorkspaceSlug(headers: ProxyHeaders, pathAndQuery: string) {
  const headerSlug = singleHeader(headers, 'x-workspace-slug');
  const querySlug = new URL(pathAndQuery, 'http://tag-gateway.invalid')
    .searchParams.get('workspace_slug');
  const referer = singleHeader(headers, 'referer');
  let refererSlug: string | undefined;
  if (referer) {
    try {
      const refererUrl = new URL(referer);
      refererSlug =
        safeWorkspaceSlug(
          refererUrl.pathname.match(/^\/tag\/([^/]+)(?:\/|$)/u)?.[1]
        ) ??
        (refererUrl.pathname === '/tag-entry'
          ? (refererUrl.searchParams.get('workspace') ?? undefined)
          : undefined);
    } catch {
      throw new Error('Tag workspace scope unavailable');
    }
  }
  const candidates = [headerSlug, querySlug, refererSlug].filter(
    (slug): slug is string => Boolean(slug)
  );
  if (new Set(candidates).size > 1) {
    throw new Error('Tag workspace scope mismatch');
  }
  const workspaceSlug = safeWorkspaceSlug(candidates[0]);
  if (!workspaceSlug) throw new Error('Tag workspace scope unavailable');
  return workspaceSlug;
}

function validAssertionPart(value: string) {
  return value.length > 0 && value.length <= 24 * 1024 && !/[\r\n]/u.test(value);
}

export async function authorizeTagGatewayForward(input: {
  readonly browserHeaders: ProxyHeaders;
  readonly audience: TagGatewayAudience;
  readonly method: string;
  readonly pathAndQuery: string;
  readonly bodySha256: string;
  readonly mint: (
    request: TagGatewayMintRequest
  ) => Promise<TagGatewayMintResponse | null>;
}): Promise<ProxyHeaders> {
  const assertion = await input.mint({
    audience: input.audience,
    method: input.method,
    pathAndQuery: input.pathAndQuery,
    workspaceSlug: resolveWorkspaceSlug(
      input.browserHeaders,
      input.pathAndQuery
    ),
    bodySha256: input.bodySha256,
  });
  if (
    !assertion ||
    !validAssertionPart(assertion.assertion) ||
    !validAssertionPart(assertion.signature) ||
    !validAssertionPart(assertion.keyId)
  ) {
    throw new Error('Tag authority unavailable');
  }
  return {
    ...sanitizeTagProxyHeaders(input.browserHeaders),
    'x-vibes-tag-assertion': assertion.assertion,
    'x-vibes-tag-assertion-signature': assertion.signature,
    'x-vibes-tag-assertion-key-id': assertion.keyId,
  } satisfies ProxyHeaders;
}
