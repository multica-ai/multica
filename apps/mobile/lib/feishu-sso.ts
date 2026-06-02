/**
 * High-level wrapper around the FeishuSSOModule native iOS module
 * (modules/feishu-sso/). Encapsulates:
 *   - one-time LarkSSO.register call (lazy, idempotent — subsequent calls
 *     skip the native round-trip)
 *   - iOS-only Platform guard (the module throws at module load on
 *     Android since the native side doesn't exist there)
 *   - derivation of the redirect_uri the backend forwards to Feishu
 *     during the code-to-token exchange. Feishu validates that
 *     redirect_uri matches the value the SDK used internally, which the
 *     SDK derives from the app id (`cli_xxx` → `clixxx://`). Sending the
 *     same value through /auth/feishu keeps the exchange working without
 *     a server-side FEISHU_REDIRECT_URI env override.
 */
import { Platform } from "react-native";
import FeishuSSO from "../modules/feishu-sso";

const FEISHU_APP_ID = process.env.EXPO_PUBLIC_FEISHU_APP_ID;

let registered = false;

async function ensureRegistered(): Promise<string> {
  if (!FEISHU_APP_ID) {
    throw new Error(
      "EXPO_PUBLIC_FEISHU_APP_ID is not set. Add it to apps/mobile/.env.staging.",
    );
  }
  if (Platform.OS !== "ios") {
    throw new Error("Feishu SSO is currently iOS-only.");
  }
  if (!registered) {
    // "en" forces the SDK's H5 fallback login page (shown when the Feishu
    // app isn't installed, e.g. the simulator) to render in English,
    // matching the app's English UI and avoiding CJK tofu on simulator
    // runtimes that ship without Chinese fonts. On a real device the SDK
    // jumps to the Feishu app and this has no effect. Change to "zh" here
    // (JS only, no native rebuild) if a Chinese login page is preferred.
    await FeishuSSO.register(FEISHU_APP_ID, "en");
    registered = true;
  }
  return FEISHU_APP_ID;
}

export interface FeishuLoginResult {
  /** OAuth authorization code, ~5min TTL, single-use. POST to /auth/feishu
   *  immediately. */
  code: string;
  /** redirect_uri value the SDK used internally. Mirror this in the
   *  /auth/feishu request body so Feishu's token-exchange API accepts
   *  the swap. */
  redirectUri: string;
}

/**
 * Start the Feishu OAuth flow. Jumps to the Feishu app on devices where
 * it's installed (Universal-Link-backed handoff handled by the SDK),
 * falls back to a SDK-managed H5 page otherwise.
 *
 * Rejection codes the caller may want to special-case:
 *   - "FEISHU_SSO_CANCELLED" — user dismissed the consent screen
 *   - "FEISHU_SSO_BUSY"       — another sign-in is already in flight
 *
 * Everything else surfaces as "FEISHU_SSO_ERROR" with the SDK's message.
 * The login screen treats cancelled as a silent reset; other errors flow
 * through mapAuthError as banner copy.
 */
export async function startFeishuLogin(): Promise<FeishuLoginResult> {
  const appId = await ensureRegistered();
  const { code } = await FeishuSSO.start([]);
  const scheme = appId.replace(/_/g, "");
  return { code, redirectUri: `${scheme}://oauth-callback` };
}
