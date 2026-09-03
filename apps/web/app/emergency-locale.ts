import { matchLocale, type SupportedLocale } from "@multica/core/i18n";
import { createBrowserCookieLocaleAdapter } from "@multica/core/i18n/browser";

export function resolveEmergencyLocale(): SupportedLocale {
  const adapter = createBrowserCookieLocaleAdapter();
  let cookieLocale: string | null = null;
  let browserLocales: readonly string[] = [];

  try {
    cookieLocale = adapter.getUserChoice();
  } catch {
    // A blocked cookie should not be able to break the last-resort UI.
  }

  try {
    browserLocales = adapter.getSystemPreferences();
  } catch {
    // Browser language detection is best-effort in the emergency boundary.
  }

  return matchLocale(
    [cookieLocale ?? "", ...browserLocales].filter(Boolean),
  );
}
