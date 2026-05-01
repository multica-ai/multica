"use client";

import { Suspense, useMemo } from "react";
import { CoreProvider } from "@multica/core/platform";
import packageJson from "../package.json";
import { WebNavigationProvider } from "@/platform/navigation";
import {
  setLoggedInCookie,
  clearLoggedInCookie,
} from "@/features/auth/auth-cookie";
import { PageviewTracker } from "./pageview-tracker";

// Token in localStorage → keep this session in token mode (e.g. Electron
// desktop, CLI-auth flow). Users on cookie auth don't hit this path.
// The forge_token key replaced the legacy multica_token key at Forge launch.
function hasLegacyToken(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return Boolean(window.localStorage.getItem("forge_token"));
  } catch {
    return false;
  }
}

// Derive WebSocket URL from the page origin so self-hosted / LAN deployments
// work without explicit NEXT_PUBLIC_WS_URL.  The Next.js rewrite rule
// (/ws → backend) handles proxying.
function deriveWsUrl(): string | undefined {
  if (process.env.NEXT_PUBLIC_WS_URL) return process.env.NEXT_PUBLIC_WS_URL;
  if (typeof window === "undefined") return undefined;
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/ws`;
}

// Build-time version preferred (CI sets NEXT_PUBLIC_APP_VERSION to a git tag
// or sha so different deploys are distinguishable in server logs); fall back
// to the package.json version so local dev still reports something useful.
const WEB_VERSION =
  process.env.NEXT_PUBLIC_APP_VERSION || packageJson.version || "dev";

export function WebProviders({ children }: { children: React.ReactNode }) {
  const cookieAuth = !hasLegacyToken();
  // Stable identity reference so downstream effects keyed on it don't see a
  // new object on every parent render.
  const identity = useMemo(
    () => ({ platform: "web", version: WEB_VERSION }),
    [],
  );
  return (
    <CoreProvider
      apiBaseUrl={process.env.NEXT_PUBLIC_API_URL}
      wsUrl={deriveWsUrl()}
      cookieAuth={cookieAuth}
      onLogin={setLoggedInCookie}
      onLogout={clearLoggedInCookie}
      identity={identity}
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
