import type { ReactNode } from "react";
import type { OAuthProviderRuntimeConfig } from "@multica/core/config";
import type { OAuthProviderButton } from "./login-page";

/** Static label + icon for each known OAuth provider id.
 *  Shared between the web and desktop login pages so new providers only need
 *  to be registered once. Providers not listed here are silently skipped by
 *  both apps, which means the server can advertise a provider before the
 *  frontends ship a button for it. */
export const PROVIDER_BUTTON_META: Record<
  string,
  { label: string; icon: ReactNode }
> = {
  google: {
    label: "Continue with Google",
    icon: (
      <svg className="mr-2 h-4 w-4" viewBox="0 0 24 24">
        <path
          d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z"
          fill="#4285F4"
        />
        <path
          d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
          fill="#34A853"
        />
        <path
          d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
          fill="#FBBC05"
        />
        <path
          d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
          fill="#EA4335"
        />
      </svg>
    ),
  },
};

/** Builds the list of OAuth buttons to render. Filters out providers the
 *  frontend doesn't have visuals for (see `PROVIDER_BUTTON_META`) and pairs
 *  each remaining provider with the caller-supplied `onLogin` strategy — web
 *  starts an authorize redirect, desktop opens the web login in a browser. */
export function buildOAuthProviderButtons(
  oauthProviders: Record<string, OAuthProviderRuntimeConfig>,
  onLogin: (id: string, cfg: OAuthProviderRuntimeConfig) => void | Promise<void>,
): OAuthProviderButton[] {
  return Object.entries(oauthProviders)
    .filter(([id]) => id in PROVIDER_BUTTON_META)
    .map(([id, cfg]) => ({
      id,
      ...PROVIDER_BUTTON_META[id]!,
      onLogin: () => onLogin(id, cfg),
    }));
}
