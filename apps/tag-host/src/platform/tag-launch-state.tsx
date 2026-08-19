import { useEffect } from 'react';

const ONE_YEAR_SECONDS = 60 * 60 * 24 * 365;

export function TagLaunchState({
  workspaceSlug,
}: {
  readonly workspaceSlug: string;
}) {
  useEffect(() => {
    const secure = window.location.protocol === 'https:' ? '; Secure' : '';
    document.cookie = `last_workspace_slug=${encodeURIComponent(workspaceSlug)}; path=/; max-age=${ONE_YEAR_SECONDS}; SameSite=Lax${secure}`;
  }, [workspaceSlug]);

  return null;
}
