import { DEFAULT_LOCALE, SUPPORTED_LOCALES, type SupportedLocale } from "@multica/core/i18n";

export interface DeviceLocale {
  languageCode: string | null;
  scriptCode?: string | null;
  regionCode: string | null;
}

const TRADITIONAL_CHINESE_REGIONS = new Set(["TW", "HK", "MO"]);

// Matched by hand rather than through core's matchLocale: Hermes ships without
// Intl.Locale, and the matcher quietly falls back to English when it throws.
export function resolveLocale(preferences: readonly DeviceLocale[]): SupportedLocale {
  for (const { languageCode, scriptCode, regionCode } of preferences) {
    const language = languageCode?.toLowerCase();
    if (language === "zh") {
      const traditional =
        scriptCode === "Hant" || (!scriptCode && TRADITIONAL_CHINESE_REGIONS.has(regionCode ?? ""));
      return traditional ? "zh-Hant" : "zh-Hans";
    }
    const supported = SUPPORTED_LOCALES.find((locale) => locale === language);
    if (supported) return supported;
  }
  return DEFAULT_LOCALE;
}
