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

type ProxyHeaders = Record<string, string | string[] | undefined>;

const TAG_COOKIE_ALLOWLIST = new Set(['multica_auth', 'multica_csrf']);

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
  const sanitized = { ...headers };
  delete sanitized.authorization;
  delete sanitized['proxy-authorization'];

  const cookieHeader = Array.isArray(headers.cookie)
    ? headers.cookie.join(';')
    : headers.cookie;
  const cookies = cookieHeader
    ?.split(';')
    .map((cookie) => cookie.trim())
    .filter((cookie) =>
      TAG_COOKIE_ALLOWLIST.has(cookie.split('=', 1)[0] ?? '')
    )
    .join('; ');
  if (cookies) sanitized.cookie = cookies;
  else delete sanitized.cookie;
  return sanitized;
}
