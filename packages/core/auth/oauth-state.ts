import { api } from "../api";
import type { OAuthProviderRuntimeConfig } from "../config";

/**
 * OAuth `state` parameter encoder/decoder. Keeping the format in one place so
 * the login page's builder and the callback page's parser can't drift apart.
 */

export interface OAuthStateParts {
  providerId?: string;
  /** "desktop" when the login was kicked off from the desktop app's browser handoff. */
  platform?: string;
  /** Safe relative next-url. Caller is responsible for sanitising before encoding and after decoding. */
  next?: string;
  /** CSRF nonce issued by POST /auth/oauth/{provider}/start; must round-trip
   *  through the provider and match the cookie the server set at /start. */
  nonce?: string;
}

export function encodeOAuthState(parts: OAuthStateParts): string {
  const params = new URLSearchParams();
  if (parts.providerId) params.set("provider", parts.providerId);
  if (parts.platform) params.set("platform", parts.platform);
  if (parts.next) params.set("next", parts.next);
  if (parts.nonce) params.set("nonce", parts.nonce);
  return params.toString();
}

export function decodeOAuthState(raw: string | null | undefined): OAuthStateParts {
  if (!raw) return {};
  const params = new URLSearchParams(raw);
  return {
    providerId: params.get("provider") ?? undefined,
    platform: params.get("platform") ?? undefined,
    next: params.get("next") ?? undefined,
    nonce: params.get("nonce") ?? undefined,
  };
}

const DESKTOP_DEEP_LINK = "multica://auth/callback";

/** Builds the `multica://` URL used to hand a JWT back to the desktop app. */
export function buildDesktopDeepLink(token: string): string {
  return `${DESKTOP_DEEP_LINK}?token=${encodeURIComponent(token)}`;
}

/** Builds the full provider authorize URL from runtime config + state. */
export function buildAuthorizeUrl(
  cfg: OAuthProviderRuntimeConfig,
  state: string,
  redirectUri: string,
): string {
  const params = new URLSearchParams({
    client_id: cfg.clientId,
    redirect_uri: redirectUri,
    response_type: "code",
    scope: cfg.scope,
    ...(cfg.extraAuthParams ?? {}),
    state,
  });
  return `${cfg.authorizeUrl}?${params.toString()}`;
}

export interface StartOAuthOptions {
  providerId: string;
  cfg: OAuthProviderRuntimeConfig;
  /** "desktop" when the flow was kicked off from the desktop app. */
  platform?: "desktop";
  /** Safe relative next-url, pre-sanitised. */
  next?: string | null;
}

/**
 * Starts an OAuth flow: asks the server to mint a CSRF nonce (which also sets
 * the cookie the server will later verify) and returns the authorize URL to
 * navigate to. All provider-agnostic.
 */
export async function startOAuthRedirect(opts: StartOAuthOptions): Promise<string> {
  const { nonce } = await api.oauthStart(opts.providerId);
  const state = encodeOAuthState({
    providerId: opts.providerId,
    platform: opts.platform,
    next: opts.next ?? undefined,
    nonce,
  });
  const redirectUri = `${window.location.origin}${opts.cfg.callbackPath}`;
  return buildAuthorizeUrl(opts.cfg, state, redirectUri);
}
