import {
  mergeResources,
  type LocaleResources,
  type SupportedLocale,
} from "@multica/core/i18n";
import en from "./brand/en.json";
import zhHans from "./brand/zh-Hans.json";
import zhHant from "./brand/zh-Hant.json";
import ko from "./brand/ko.json";
import ja from "./brand/ja.json";
import fr from "./brand/fr.json";

export const BRAND_OVERRIDES: Record<SupportedLocale, LocaleResources> = {
  en,
  "zh-Hans": zhHans,
  "zh-Hant": zhHant,
  ko,
  ja,
  fr,
};

export function applyBrandOverrides(
  resources: Record<SupportedLocale, LocaleResources>,
): Record<SupportedLocale, LocaleResources> {
  return mergeResources(resources, BRAND_OVERRIDES);
}
