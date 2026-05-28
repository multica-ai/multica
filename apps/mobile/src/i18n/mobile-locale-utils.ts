import { matchLocale, type SupportedLocale } from "@multica/core/i18n";

const DEFAULT_MOBILE_LOCALE: SupportedLocale = "zh-Hans";

export function resolveInitialMobileLocale(userChoice: string | null): SupportedLocale {
  if (userChoice) return matchLocale([userChoice]);
  return DEFAULT_MOBILE_LOCALE;
}
