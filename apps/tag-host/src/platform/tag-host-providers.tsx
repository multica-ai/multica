import { useMemo, type ReactNode } from 'react';
import { CoreProvider } from '@multica/core/platform';
import { pickLocale } from '@multica/core/i18n';
import { createBrowserCookieLocaleAdapter } from '@multica/core/i18n/browser';
import { ThemeProvider } from '@multica/ui/components/common/theme-provider';
import { RESOURCES } from '@multica/views/locales';
import { resolveTagRuntimeUrls } from './paths';
import { TagNavigationProvider } from './tag-navigation-provider';
import { createTagTaskResources } from './tag-task-resources';

export function TagHostProviders({ children }: { children: ReactNode }) {
  const runtime = useMemo(
    () => resolveTagRuntimeUrls(window.location.origin),
    []
  );
  const resources = useMemo(
    () =>
      Object.fromEntries(
        Object.entries(RESOURCES).map(([locale, localeResources]) => [
          locale,
          createTagTaskResources(localeResources),
        ])
      ) as typeof RESOURCES,
    []
  );
  const localeAdapter = useMemo(createBrowserCookieLocaleAdapter, []);
  const locale = useMemo(() => pickLocale(localeAdapter), [localeAdapter]);
  const identity = useMemo(
    () => ({ platform: 'vibes-tag-host', version: 'tracer-258' }),
    []
  );

  return (
    <ThemeProvider>
      <CoreProvider
        apiBaseUrl={runtime.apiBaseUrl}
        wsUrl={runtime.wsUrl}
        cookieAuth
        identity={identity}
        locale={locale}
        resources={resources}
        localeAdapter={localeAdapter}
      >
        <TagNavigationProvider>{children}</TagNavigationProvider>
      </CoreProvider>
    </ThemeProvider>
  );
}
