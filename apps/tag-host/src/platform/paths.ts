const TAG_PREFIX = '/tag';

function withLeadingSlash(path: string) {
  return path.startsWith('/') ? path : `/${path}`;
}

function mountProjectPathInTasks(path: string) {
  const url = new URL(path, 'https://tag.local');
  const match = url.pathname.match(
    /^\/([^/]+)\/projects(?:\/([^/]+))?\/?$/u
  );
  if (!match) return `${url.pathname}${url.search}${url.hash}`;

  const [, workspaceSlug, projectId] = match;
  if (projectId) {
    url.pathname = `/${workspaceSlug}/issues/projects/${projectId}`;
  } else {
    url.pathname = `/${workspaceSlug}/issues`;
    url.searchParams.set('tab', 'projects');
  }
  return `${url.pathname}${url.search}${url.hash}`;
}

export function toTagHostPath(path: string) {
  const normalized = withLeadingSlash(path);
  if (
    normalized === TAG_PREFIX ||
    normalized.startsWith(`${TAG_PREFIX}/`)
  ) {
    return normalized;
  }
  return `${TAG_PREFIX}${mountProjectPathInTasks(normalized)}`;
}

export function fromTagHostLocation(pathname: string, search: string) {
  const normalized = withLeadingSlash(pathname);
  const innerPathname = normalized.startsWith(`${TAG_PREFIX}/`)
    ? normalized.slice(TAG_PREFIX.length)
    : normalized;
  const searchParams = new URLSearchParams(search);
  const projectDetail = innerPathname.match(
    /^\/([^/]+)\/issues\/projects\/([^/]+)\/?$/u
  );
  const projectList = innerPathname.match(/^\/([^/]+)\/issues\/?$/u);
  const canonicalPathname = projectDetail
    ? `/${projectDetail[1]}/projects/${projectDetail[2]}`
    : projectList && searchParams.get('tab') === 'projects'
      ? `/${projectList[1]}/projects`
      : innerPathname || '/';
  return { pathname: canonicalPathname, searchParams };
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
