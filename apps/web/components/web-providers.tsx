"use client";

import { useEffect, useMemo } from "react";
import dynamic from "next/dynamic";
import { CoreProvider } from "@multica/core/platform";
import { installUserTimingCap } from "@multica/core/diagnostics";
import { createBrowserCookieLocaleAdapter } from "@multica/core/i18n/browser";
import type { SupportedLocale } from "@multica/core/i18n";
import packageJson from "../package.json";
import { LocaleBundle } from "@/components/i18n/locale-bundle";
import { WebNavigationProvider } from "@/platform/navigation";
import { WebScrollRestorationProvider } from "@/platform/scroll-restoration";
import {
  setLoggedInCookie,
  clearLoggedInCookie,
} from "@/features/auth/auth-cookie";
import { detectWebOS } from "@/platform/client-os";
import { useUserLocaleSyncEnabled } from "@/platform/user-locale-sync";

// Client-only: every reading is a live client measurement the server cannot
// render, so server HTML would only ever mismatch on hydration.
const WebDevToolbar = dynamic(
  () => import("./web-dev-toolbar").then((m) => m.WebDevToolbar),
  { ssr: false },
);

// Legacy token in localStorage → keep this session in token mode so users who
// logged in before the cookie-auth migration stay authed. They migrate to
// cookie mode on their next logout/login cycle (logout clears multica_token).
// Sunset: once telemetry shows <1% of sessions still carry multica_token,
// delete this branch and hard-code `cookieAuth` — the localStorage token is
// XSS-exposed and is the exact thing the cookie migration exists to remove.
function hasLegacyToken(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return Boolean(window.localStorage.getItem("multica_token"));
  } catch {
    return false;
  }
}

// Derive WebSocket URL from the page origin so self-hosted / LAN deployments
// work without an explicit runtime wsUrl. The Next.js runtime proxy handles
// /ws -> backend when the deployment keeps WebSockets same-origin.
function deriveWsUrl(): string | undefined {
  if (typeof window === "undefined") return undefined;
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/ws`;
}

// Build-time version preferred (CI sets NEXT_PUBLIC_APP_VERSION to a git tag
// or sha so different deploys are distinguishable in server logs); fall back
// to the package.json version so local dev still reports something useful.
const WEB_VERSION =
  process.env.NEXT_PUBLIC_APP_VERSION || packageJson.version || "dev";

export function WebProviders({
  children,
  locale,
  apiBaseUrl,
  wsUrl,
  devToolbarApiUrl,
}: {
  children: React.ReactNode;
  locale: SupportedLocale;
  apiBaseUrl?: string;
  wsUrl?: string;
  /** Set only when the dev toolbar is enabled; the backend it reports. */
  devToolbarApiUrl?: string;
}) {
  const cookieAuth = !hasLegacyToken();
  const syncUserLocale = useUserLocaleSyncEnabled();
  // Stable identity reference so downstream effects keyed on it don't see a
  // new object on every parent render.
  const identity = useMemo(
    () => ({ platform: "web", version: WEB_VERSION, os: detectWebOS() }),
    [],
  );
  const localeAdapter = useMemo(() => createBrowserCookieLocaleAdapter(), []);
  // React's development build never clears its render measures; see
  // installUserTimingCap.
  useEffect(() => {
    if (process.env.NODE_ENV === "development") installUserTimingCap();
  }, []);
  return (
    <LocaleBundle locale={locale}>
      {(resources) => (
        <CoreProvider
          apiBaseUrl={apiBaseUrl}
          wsUrl={wsUrl || deriveWsUrl()}
          cookieAuth={cookieAuth}
          onLogin={setLoggedInCookie}
          onLogout={clearLoggedInCookie}
          identity={identity}
          locale={locale}
          resources={resources}
          localeAdapter={localeAdapter}
          syncUserLocale={syncUserLocale}
        >
          <WebNavigationProvider>
            <WebScrollRestorationProvider>{children}</WebScrollRestorationProvider>
            {process.env.NODE_ENV === "development" && devToolbarApiUrl && (
              <WebDevToolbar apiUrl={devToolbarApiUrl} />
            )}
          </WebNavigationProvider>
        </CoreProvider>
      )}
    </LocaleBundle>
  );
}
