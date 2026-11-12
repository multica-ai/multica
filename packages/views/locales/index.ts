import type { LocaleResources, SupportedLocale } from "@multica/core/i18n";
import en from "./en";
import zhHans from "./zh-Hans";
import zhHant from "./zh-Hant";
import ko from "./ko";
import ja from "./ja";
import ru from "./ru";
import fr from "./fr";
import es from "./es";

export const RESOURCES: Record<SupportedLocale, LocaleResources> = {
  en,
  "zh-Hans": zhHans,
  "zh-Hant": zhHant,
  ko,
  ja,
  ru,
  fr,
  es,
};
