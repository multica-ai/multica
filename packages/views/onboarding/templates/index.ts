import {
  matchLocale,
  type SupportedLocale,
} from "@multica/core/i18n";

export {
  INSTALL_RUNTIME_ISSUE_TITLE,
  INSTALL_RUNTIME_ISSUE_BODY,
} from "./install-runtime-issue";
export {
  getMikaOnboarding,
  type MikaContentLang,
  type MikaOnboardingDefinition,
} from "./mika";
type ContentLang = "en" | "zh" | "ko" | "ja";

const CONTENT_LANG_BY_LOCALE: Record<SupportedLocale, ContentLang> = {
  en: "en",
  "zh-Hans": "zh",
  ko: "ko",
  ja: "ja",
};

/**
 * Pick persisted onboarding content for the given user language. Maps
 * supported BCP-47 prefixes to the matching variant; everything else falls
 * back to English. Mirrors the locale picker used by the frontend i18n layer.
 */
export function pickContentLang(
  language: string | null | undefined,
): ContentLang {
  return CONTENT_LANG_BY_LOCALE[matchLocale(language ? [language] : [])];
}
