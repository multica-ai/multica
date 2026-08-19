import { useEffect } from 'react';
import { useAuthStore } from '@multica/core/auth';

interface TagSessionRecoveryInput {
  readonly isLoading: boolean;
  readonly hasUser: boolean;
  readonly workspaceSlug: string;
  readonly pathname: string;
  readonly search: string;
  readonly hash: string;
}

export function resolveTagSessionRecovery({
  isLoading,
  hasUser,
  workspaceSlug,
  pathname,
  search,
  hash,
}: TagSessionRecoveryInput) {
  if (isLoading || hasUser) return null;
  const workspacePrefix = `/tag/${encodeURIComponent(workspaceSlug)}`;
  const relativePath = pathname.startsWith(`${workspacePrefix}/`)
    ? pathname.slice(workspacePrefix.length)
    : '/chat';
  const entry = new URL('/tag-entry', 'https://tag.local');
  entry.searchParams.set('workspace', workspaceSlug);
  entry.searchParams.set('page', `${relativePath}${search}${hash}`);
  return `${entry.pathname}${entry.search}`;
}

export function TagSessionRecovery({
  workspaceSlug,
}: {
  readonly workspaceSlug: string;
}) {
  const user = useAuthStore((state) => state.user);
  const isLoading = useAuthStore((state) => state.isLoading);
  const redirect = resolveTagSessionRecovery({
    isLoading,
    hasUser: Boolean(user),
    workspaceSlug,
    pathname: window.location.pathname,
    search: window.location.search,
    hash: window.location.hash,
  });

  useEffect(() => {
    if (redirect) window.location.replace(redirect);
  }, [redirect]);

  return null;
}
