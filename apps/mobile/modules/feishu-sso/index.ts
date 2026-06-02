// Typed JS binding for the FeishuSSOModule native iOS module.
//
// The `Name("FeishuSSOModule")` declaration in
// ios/FeishuSSOModule.swift is the contract this requireNativeModule call
// resolves against — keep the strings in sync.
//
// All methods are iOS-only. On Android (or a Metro bundle that somehow
// landed in a non-iOS context), `requireNativeModule` throws at first
// access. Callers in lib/feishu-sso.ts guard with a Platform.OS check.
import { requireNativeModule } from "expo-modules-core";

interface FeishuSSONative {
  /**
   * One-time SDK registration. Idempotent: calling more than once with
   * the same app id is a no-op inside LarkSSO. The native side derives
   * the URL scheme from `appId` by stripping the underscore.
   *
   * `lang` sets the language of the SDK's H5 fallback login page
   * ("en" / "zh" / ...). Pass "" to let the SDK choose from the system.
   */
  register(appId: string, lang: string): Promise<void>;
  /**
   * Open the Feishu OAuth flow — jumps to the Feishu app if installed,
   * otherwise the SDK renders its own H5 fallback. Resolves with the
   * authorization `code` on success; rejects with one of:
   *   - "FEISHU_SSO_CANCELLED" — user dismissed the consent screen
   *   - "FEISHU_SSO_BUSY"       — another sign-in is already in flight
   *   - "FEISHU_SSO_NO_VIEW_CONTROLLER" — no host VC found at call time
   *   - "FEISHU_SSO_ERROR"      — anything else, with the SDK's message
   */
  start(scope: string[]): Promise<{ code: string }>;
}

export default requireNativeModule<FeishuSSONative>("FeishuSSOModule");
