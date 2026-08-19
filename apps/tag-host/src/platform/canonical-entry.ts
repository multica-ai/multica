export interface CanonicalTagRequest {
  readonly pathname: string;
  readonly search: string;
  readonly cookie?: string;
}

export interface TagWebManifest {
  readonly id: string;
  readonly name: string;
  readonly short_name: string;
  readonly description: string;
  readonly start_url: string;
  readonly scope: string;
  readonly display: 'standalone';
  readonly background_color: string;
  readonly theme_color: string;
  readonly icons: ReadonlyArray<{
    readonly src: string;
    readonly sizes: string;
    readonly type: string;
    readonly purpose?: string;
  }>;
}

const WORKSPACE_SLUG = /^[a-z0-9][a-z0-9-]{0,127}$/u;
const DIRECT_SURFACES = new Set([
  'chat',
  'issues',
  'agents',
  'runtimes',
  'settings',
]);
const MIGRATING_SURFACES = new Set(['inbox', 'members']);
const SLUGLESS_COMPATIBILITY_SURFACES = new Set([
  'chat',
  'issues',
  'projects',
  'agents',
  'squads',
  'my-issues',
  'autopilots',
  'runtimes',
]);

function readCookie(cookieHeader: string | undefined, name: string) {
  const encoded = cookieHeader
    ?.split(';')
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${name}=`))
    ?.slice(name.length + 1);
  if (!encoded) return null;
  try {
    return decodeURIComponent(encoded);
  } catch {
    return null;
  }
}

function safeWorkspaceSlug(value: string | null | undefined) {
  return value && WORKSPACE_SLUG.test(value) ? value : null;
}

function safeDecodePathSegment(value: string | undefined) {
  try {
    return decodeURIComponent(value ?? '');
  } catch {
    return null;
  }
}

function canonicalWorkspacePath(
  workspaceSlug: string,
  surface: string,
  rest: readonly string[],
  search: string
) {
  const encodedSlug = encodeURIComponent(workspaceSlug);
  const suffix = rest.length > 0 ? `/${rest.join('/')}` : '';
  const url = new URL(`/tag/${encodedSlug}/chat`, 'https://tag.local');

  if (DIRECT_SURFACES.has(surface)) {
    url.pathname = `/tag/${encodedSlug}/${surface}${suffix}`;
  } else if (MIGRATING_SURFACES.has(surface)) {
    url.pathname = `/tag/${encodedSlug}/${surface}${suffix}`;
  } else if (surface === 'projects') {
    if (rest.length > 0) {
      url.pathname = `/tag/${encodedSlug}/issues/projects${suffix}`;
    } else {
      url.pathname = `/tag/${encodedSlug}/issues`;
      url.searchParams.set('tab', 'projects');
    }
  } else if (surface === 'squads') {
    url.pathname = `/tag/${encodedSlug}/agents/teams${suffix}`;
  } else if (surface === 'my-issues') {
    url.pathname = `/tag/${encodedSlug}/issues`;
    url.searchParams.set('tab', 'my');
  } else if (surface === 'autopilots') {
    if (rest.length > 0) {
      url.pathname = `/tag/${encodedSlug}/issues/automations${suffix}`;
    } else {
      url.pathname = `/tag/${encodedSlug}/issues`;
      url.searchParams.set('tab', 'automations');
    }
  } else {
    return null;
  }

  const incoming = new URLSearchParams(search);
  for (const [key, value] of incoming) {
    if (!url.searchParams.has(key)) url.searchParams.append(key, value);
  }
  return `${url.pathname}${url.search}`;
}

function resolveLauncher(search: string, cookie: string | undefined) {
  const searchParams = new URLSearchParams(search);
  const requestedWorkspace = safeWorkspaceSlug(searchParams.get('workspace'));
  if (requestedWorkspace) {
    const page = searchParams.get('page');
    const entry = new URL('/tag-entry', 'https://tag.local');
    entry.searchParams.set('workspace', requestedWorkspace);
    entry.searchParams.set(
      'page',
      page?.startsWith('/') && !page.startsWith('//') ? page : '/chat'
    );
    return `${entry.pathname}${entry.search}`;
  }

  const lastWorkspace = safeWorkspaceSlug(
    readCookie(cookie, 'last_workspace_slug')
  );
  if (!lastWorkspace) return '/tag-entry';
  if (readCookie(cookie, 'multica_auth')) {
    return `/tag/${encodeURIComponent(lastWorkspace)}/chat`;
  }
  const entry = new URL('/tag-entry', 'https://tag.local');
  entry.searchParams.set('workspace', lastWorkspace);
  return `${entry.pathname}${entry.search}`;
}

export function resolveCanonicalTagRequest({
  pathname,
  search,
  cookie,
}: CanonicalTagRequest): { readonly redirect: string } | null {
  const normalizedPathname = pathname.length > 1
    ? pathname.replace(/\/+$/u, '')
    : pathname;
  if (normalizedPathname === '/tag') {
    return { redirect: resolveLauncher(search, cookie) };
  }

  const segments = normalizedPathname.split('/').filter(Boolean);
  const underTag = segments[0] === 'tag';
  const candidate = underTag ? segments.slice(1) : segments;

  if (candidate.length >= 2) {
    const workspaceSlug = safeWorkspaceSlug(
      safeDecodePathSegment(candidate[0])
    );
    const surface = candidate[1] ?? '';
    if (workspaceSlug) {
      const canonical = canonicalWorkspacePath(
        workspaceSlug,
        surface,
        candidate.slice(2),
        search
      );
      const current = `${normalizedPathname}${search}`;
      if (canonical && canonical !== current) return { redirect: canonical };
    }
  }

  if (!underTag && candidate.length >= 1) {
    const surface = candidate[0] ?? '';
    const lastWorkspace = safeWorkspaceSlug(
      readCookie(cookie, 'last_workspace_slug')
    );
    if (lastWorkspace && SLUGLESS_COMPATIBILITY_SURFACES.has(surface)) {
      const canonical = canonicalWorkspacePath(
        lastWorkspace,
        surface,
        candidate.slice(1),
        search
      );
      if (canonical) return { redirect: canonical };
    }
  }

  return null;
}

export function createTagManifest(): TagWebManifest {
  return {
    id: '/tag/',
    name: 'VIBES Tag',
    short_name: 'Tag',
    description: 'Local Agent collaboration in VIBES.',
    start_url: '/tag/',
    scope: '/tag/',
    display: 'standalone',
    background_color: '#ffffff',
    theme_color: '#ffffff',
    icons: [
      {
        src: '/vibes-pwa-192x192.png',
        sizes: '192x192',
        type: 'image/png',
        purpose: 'any',
      },
      {
        src: '/vibes-pwa-512x512.png',
        sizes: '512x512',
        type: 'image/png',
        purpose: 'any',
      },
    ],
  };
}

export type TagMigrationSurface = 'inbox' | 'members';

export function createTagMigrationPage({
  workspaceSlug,
  surface,
}: {
  readonly workspaceSlug: string;
  readonly surface: TagMigrationSurface;
}) {
  const label = surface === 'inbox' ? 'Inbox' : 'Members';
  const chatPath = `/tag/${encodeURIComponent(workspaceSlug)}/chat`;
  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>${label} · Migrating to Tag</title>
    <style>
      :root { color-scheme: light dark; font-family: ui-sans-serif, system-ui, sans-serif; }
      body { margin: 0; min-height: 100dvh; display: grid; place-items: center; background: Canvas; color: CanvasText; }
      main { width: min(28rem, calc(100% - 2rem)); text-align: center; }
      p { color: color-mix(in srgb, CanvasText 65%, transparent); line-height: 1.5; }
      a { color: inherit; font-weight: 600; }
    </style>
  </head>
  <body>
    <main>
      <h1>${label} is Migrating to Tag</h1>
      <p>This workspace surface is not available in the TanStack host yet. It has not fallen back to the old Next host.</p>
      <a href="${chatPath}">Return to Tag Chat</a>
    </main>
  </body>
</html>`;
}
