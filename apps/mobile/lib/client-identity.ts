/**
 * Client identity sent on every mobile request and WS upgrade.
 *
 * `X-Client-OS` / `client_os` must reflect the real RN platform — a hardcoded
 * `"ios"` attributes Android traffic to iPhone in server logs and metrics.
 * Keep this helper free of `react-native` so Node vitest can cover the
 * header contract without loading Hermes shims.
 */

export const CLIENT_PLATFORM = "mobile" as const;
export const CLIENT_VERSION = "0.1.0";

export function clientIdentityHeaders(os: string): {
  "X-Client-Platform": typeof CLIENT_PLATFORM;
  "X-Client-OS": string;
  "X-Client-Version": typeof CLIENT_VERSION;
} {
  return {
    "X-Client-Platform": CLIENT_PLATFORM,
    "X-Client-OS": os,
    "X-Client-Version": CLIENT_VERSION,
  };
}
