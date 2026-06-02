import type { SupportedLocale } from "@wallts/core/i18n";

export function docsHrefForLocale(locale: SupportedLocale): string {
  if (locale === "zh-Hans") return "/docs/zh";
  if (locale === "ko") return "/docs/ko";
  return "/docs";
}
