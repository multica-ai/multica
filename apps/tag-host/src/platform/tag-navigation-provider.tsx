import { useEffect, useMemo, type ReactNode } from 'react';
import { useRouter, useRouterState } from '@tanstack/react-router';
import {
  NavigationProvider,
  type NavigationAdapter,
} from '@multica/views/navigation';
import {
  fromTagHostLocation,
  toTagHostPath,
  toTagShareUrl,
} from './paths';

interface TagNavigationAdapterInput {
  location: { pathname: string; search: string };
  origin: string;
  navigate: (href: string, replace: boolean) => void;
  back: () => void;
  forward: () => void;
  open: (href: string) => void;
  canGoBack: () => boolean;
}

export function createTagNavigationAdapter({
  location,
  origin,
  navigate,
  back,
  forward,
  open,
  canGoBack,
}: TagNavigationAdapterInput): NavigationAdapter {
  const multicaLocation = fromTagHostLocation(
    location.pathname,
    location.search
  );
  return {
    push: (path) => navigate(toTagHostPath(path), false),
    replace: (path) => navigate(toTagHostPath(path), true),
    back,
    forward,
    canGoBack,
    pathname: multicaLocation.pathname,
    searchParams: multicaLocation.searchParams,
    openInNewTab: (path) => open(toTagHostPath(path)),
    getShareableUrl: (path) => toTagShareUrl(origin, path),
    resolveHref: toTagHostPath,
  };
}

export function TagNavigationProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const location = useRouterState({ select: (state) => state.location });

  useEffect(() => {
    const handleInternalLink = (event: Event) => {
      const detail = (
        event as CustomEvent<{ path?: string; disposition?: string }>
      ).detail;
      if (!detail?.path) return;
      const href = toTagHostPath(detail.path);
      if (
        detail.disposition === 'background-tab' ||
        detail.disposition === 'foreground-tab'
      ) {
        window.open(href, '_blank', 'noopener,noreferrer');
        return;
      }
      void router.navigate({ href });
    };
    window.addEventListener('multica:navigate', handleInternalLink);
    return () =>
      window.removeEventListener('multica:navigate', handleInternalLink);
  }, [router]);

  const adapter = useMemo<NavigationAdapter>(
    () =>
      createTagNavigationAdapter({
        location: {
          pathname: location.pathname,
          search: location.searchStr,
        },
        origin: window.location.origin,
        navigate: (href, replace) =>
          void router.navigate({ href, replace }),
        back: () => window.history.back(),
        forward: () => window.history.forward(),
        open: (href) =>
          void window.open(href, '_blank', 'noopener,noreferrer'),
        canGoBack: () => window.history.length > 1,
      }),
    [location.pathname, location.searchStr, router]
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
