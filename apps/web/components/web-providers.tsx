"use client";

import { CoreProvider } from "@multica/core/platform";
import { WebNavigationProvider } from "@/platform/navigation";
import {
  setLoggedInCookie,
  clearLoggedInCookie,
} from "@/features/auth/auth-cookie";

export function WebProviders({ children }: { children: React.ReactNode }) {
  return (
    <CoreProvider
      apiBaseUrl={process.env.NEXT_PUBLIC_API_URL}
      wsUrl={process.env.NEXT_PUBLIC_WS_URL}
      onLogin={setLoggedInCookie}
      onLogout={clearLoggedInCookie}
    >
      <WebNavigationProvider>{children}</WebNavigationProvider>
    </CoreProvider>
  );
}
