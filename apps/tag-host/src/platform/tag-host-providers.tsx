import { useMemo, type ReactNode } from 'react';
import { CoreProvider } from '@multica/core/platform';
import { RESOURCES } from '@multica/views/locales';
import { resolveTagRuntimeUrls } from './paths';
import { TagNavigationProvider } from './tag-navigation-provider';

export function TagHostProviders({ children }: { children: ReactNode }) {
  const runtime = useMemo(
    () => resolveTagRuntimeUrls(window.location.origin),
    []
  );
  const resources = useMemo(() => ({ en: RESOURCES.en }), []);
  const identity = useMemo(
    () => ({ platform: 'vibes-tag-host', version: 'tracer-258' }),
    []
  );

  return (
    <CoreProvider
      apiBaseUrl={runtime.apiBaseUrl}
      wsUrl={runtime.wsUrl}
      cookieAuth
      identity={identity}
      locale="en"
      resources={resources}
    >
      <TagNavigationProvider>{children}</TagNavigationProvider>
    </CoreProvider>
  );
}
