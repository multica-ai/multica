"use client";

import { CoreProvider } from "@multica/core/platform";
import { WebNavigationProvider } from "@/platform/navigation";
import {
  setLoggedInCookie,
  clearLoggedInCookie,
} from "@/features/auth/auth-cookie";

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

export function WebProviders({ children }: { children: React.ReactNode }) {
  const cookieAuth = !hasLegacyToken();
  return (
    <CoreProvider
      apiBaseUrl={process.env.NEXT_PUBLIC_API_URL}
      wsUrl={process.env.NEXT_PUBLIC_WS_URL}
      cookieAuth={cookieAuth}
      onLogin={setLoggedInCookie}
      onLogout={clearLoggedInCookie}
    >
      <WebNavigationProvider>{children}</WebNavigationProvider>
    </CoreProvider>
  );
}
