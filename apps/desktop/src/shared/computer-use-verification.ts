/**
 * Bundle ID prefix the verification host gives each environment's dev
 * Electron. Those apps are disposable test builds: access needs no dialog, and
 * Computer Use may drive their main process for input macOS cannot deliver in
 * the background.
 */
export const VERIFICATION_BUNDLE_PREFIX = "ai.multica.verification.";

export function isVerificationApp(appId: string): boolean {
  return appId.startsWith(VERIFICATION_BUNDLE_PREFIX) && appId.length > VERIFICATION_BUNDLE_PREFIX.length;
}
