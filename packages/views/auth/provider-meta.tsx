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
  github: {
    label: "Continue with GitHub",
    icon: (
      <svg
        className="mr-2 h-4 w-4"
        viewBox="0 0 24 24"
        fill="currentColor"
        aria-hidden="true"
      >
        <path d="M12 .5C5.73.5.5 5.73.5 12.02c0 5.08 3.29 9.39 7.86 10.91.57.11.78-.25.78-.55 0-.27-.01-1.17-.02-2.12-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.28-1.68-1.28-1.68-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.03 1.76 2.69 1.25 3.35.96.1-.74.4-1.25.73-1.54-2.55-.29-5.23-1.28-5.23-5.68 0-1.25.45-2.28 1.18-3.08-.12-.29-.51-1.46.11-3.04 0 0 .96-.31 3.15 1.18.91-.25 1.89-.38 2.86-.39.97.01 1.95.14 2.86.39 2.19-1.49 3.15-1.18 3.15-1.18.62 1.58.23 2.75.11 3.04.73.8 1.18 1.83 1.18 3.08 0 4.41-2.69 5.38-5.25 5.67.41.36.78 1.05.78 2.11 0 1.52-.01 2.75-.01 3.13 0 .31.21.67.79.55 4.56-1.53 7.85-5.84 7.85-10.91C23.5 5.73 18.27.5 12 .5z" />
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
