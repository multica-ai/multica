"use client";

import { Suspense, useMemo } from "react";
import { CoreProvider } from "@multica/core/platform";
import { createBrowserCookieLocaleAdapter } from "@multica/core/i18n/browser";
import type { LocaleResources, SupportedLocale } from "@multica/core/i18n";
import { useWelcomeStore } from "@multica/core/onboarding";
import packageJson from "../package.json";
import { WebNavigationProvider } from "@/platform/navigation";
import {
  setLoggedInCookie,
  clearLoggedInCookie,
} from "@/features/auth/auth-cookie";
import { WEB_BRAND_NAME } from "@/lib/brand";
import { PageviewTracker } from "./pageview-tracker";

const WEB_I18N_VARIABLES = { productName: WEB_BRAND_NAME };

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
// work without explicit NEXT_PUBLIC_WS_URL.  The Next.js rewrite rule
// (/ws → backend) handles proxying.
// When NEXT_PUBLIC_API_URL is a relative path (e.g. /multica-backend), the
// WS path is derived from it so both HTTP and WS requests share the same
// subpath prefix.
function deriveWsUrl(): string | undefined {
  if (process.env.NEXT_PUBLIC_WS_URL) return process.env.NEXT_PUBLIC_WS_URL;
  if (typeof window === "undefined") return undefined;
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  const apiPath = process.env.NEXT_PUBLIC_API_URL || "";
  // If apiBaseUrl is a relative path like /multica-backend, WS goes to
  // /multica-backend/ws; otherwise fall back to /ws.
  const wsPath = apiPath && apiPath.startsWith("/") ? `${apiPath}/ws` : "/ws";
  return `${proto}//${window.location.host}${wsPath}`;
}

// Build-time version preferred (CI sets NEXT_PUBLIC_APP_VERSION to a git tag
// or sha so different deploys are distinguishable in server logs); fall back
// to the package.json version so local dev still reports something useful.
const WEB_VERSION =
  process.env.NEXT_PUBLIC_APP_VERSION || packageJson.version || "dev";

export function WebProviders({
  children,
  locale,
  resources,
}: {
  children: React.ReactNode;
  locale: SupportedLocale;
  resources: Record<string, LocaleResources>;
}) {
  const cookieAuth = !hasLegacyToken();
  // Stable identity reference so downstream effects keyed on it don't see a
  // new object on every parent render.
  const identity = useMemo(
    () => ({ platform: "web", version: WEB_VERSION }),
    [],
  );
  const localeAdapter = useMemo(() => createBrowserCookieLocaleAdapter(), []);
  return (
    <CoreProvider
      apiBaseUrl={process.env.NEXT_PUBLIC_API_URL}
      wsUrl={deriveWsUrl()}
      cookieAuth={cookieAuth}
      onLogin={setLoggedInCookie}
      onLogout={() => {
        // welcome-store holds the transient post-onboarding signal. Must
        // clear on logout so user B logging into the same browser doesn't
        // inherit user A's signal and have <WelcomeAfterOnboarding /> fire
        // listAgents / createIssue against a workspace user B doesn't even
        // belong to. The store's own docstring promises this reset; this
        // is where it gets wired.
        useWelcomeStore.getState().reset();
        clearLoggedInCookie();
      }}
      identity={identity}
      locale={locale}
      resources={resources}
      i18nVariables={WEB_I18N_VARIABLES}
      localeAdapter={localeAdapter}
    >
      {/* Suspense boundary is required by Next.js for useSearchParams in
          a client component mounted this high in the tree. */}
      <Suspense fallback={null}>
        <PageviewTracker />
      </Suspense>
      <WebNavigationProvider>{children}</WebNavigationProvider>
    </CoreProvider>
  );
}
