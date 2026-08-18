const TAG_PREFIX = '/tag';

function withLeadingSlash(path: string) {
  return path.startsWith('/') ? path : `/${path}`;
}

export function toTagHostPath(path: string) {
  const normalized = withLeadingSlash(path);
  return normalized === TAG_PREFIX || normalized.startsWith(`${TAG_PREFIX}/`)
    ? normalized
    : `${TAG_PREFIX}${normalized}`;
}

export function fromTagHostLocation(pathname: string, search: string) {
  const normalized = withLeadingSlash(pathname);
  const innerPathname = normalized.startsWith(`${TAG_PREFIX}/`)
    ? normalized.slice(TAG_PREFIX.length)
    : normalized;
  return {
    pathname: innerPathname || '/',
    searchParams: new URLSearchParams(search),
  };
}

export function toTagShareUrl(origin: string, path: string) {
  return `${origin.replace(/\/$/u, '')}${toTagHostPath(path)}`;
}

export function resolveTagRuntimeUrls(origin: string) {
  const url = new URL(origin);
  const websocketProtocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return {
    apiBaseUrl: '/api/tag',
    wsUrl: `${websocketProtocol}//${url.host}/ws/tag`,
  };
}
