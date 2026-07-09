import { match } from "@formatjs/intl-localematcher";
import {
  DEFAULT_LOCALE,
  SUPPORTED_LOCALES,
  type LocaleAdapter,
  type SupportedLocale,
} from "./types";

export function matchLocale(candidates: string[]): SupportedLocale {
  if (candidates.length === 0) return DEFAULT_LOCALE;
  try {
    return match(
      candidates,
      SUPPORTED_LOCALES,
      DEFAULT_LOCALE,
    ) as SupportedLocale;
  } catch {
    return DEFAULT_LOCALE;
  }
}

export function pickLocale(adapter: LocaleAdapter): SupportedLocale {
  const choice = adapter.getUserChoice();
  if (choice) return matchLocale([choice]);
  return matchLocale(adapter.getSystemPreferences());
}
