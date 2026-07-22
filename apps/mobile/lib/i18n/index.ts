/**
 * Mobile-owned i18next init. Independent from packages/core/i18n and
 * packages/views/locales — mobile owns its own i18n per the root
 * CLAUDE.md "Mobile is independent" rule. Only two languages so far:
 * en, zh-Hans. See apps/mobile/CLAUDE.md for the full language list.
 */
import i18next from "i18next";
import { initReactI18next } from "react-i18next";
import * as Localization from "expo-localization";
import { RESOURCES, type SupportedLocale } from "@/locales";

export function detectDeviceLocale(): SupportedLocale {
  const locales = Localization.getLocales();
  const languageCode = locales[0]?.languageCode ?? "en";
  return languageCode.startsWith("zh") ? "zh-Hans" : "en";
}

void i18next.use(initReactI18next).init({
  resources: RESOURCES,
  lng: detectDeviceLocale(),
  fallbackLng: "en",
  ns: Object.keys(RESOURCES.en),
  defaultNS: "common",
  interpolation: { escapeValue: false },
});

export default i18next;
export type { SupportedLocale };
