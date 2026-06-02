import { loader } from "fumadocs-core/source";
import { defineI18n } from "fumadocs-core/i18n";
import type { SupportedLocale } from "@multica/core/i18n";
import { useCases } from "@/.source";
import { mergeUseCasePagesWithEnglishFallback } from "./use-case-locale-fallback";

// Use-case content uses dot-suffixed MDX files (`<slug>.en.mdx`,
// `<slug>.zh.mdx`, `<slug>.ko.mdx`, and `<slug>.ja.mdx`). The public route remains prefix-free; request locale is
// resolved through the same cookie/header path as the rest of the web app.
export const i18n = defineI18n({
  languages: ["en", "zh", "ko", "ja"],
  defaultLanguage: "en",
  hideLocale: "default-locale",
  parser: "dot",
});

export type UseCaseLang = (typeof i18n.languages)[number];

export function getUseCaseLangForLocale(locale: SupportedLocale): UseCaseLang {
  if (locale === "zh-Hans") return "zh";
  if (locale === "ko") return "ko";
  if (locale === "ja") return "ja";
  return "en";
}

export const useCasesSource = loader({
  baseUrl: "/usecases",
  source: useCases.toFumadocsSource(),
  i18n,
});

export function getUseCasePagesForLocale(locale: SupportedLocale) {
  const lang = getUseCaseLangForLocale(locale);
  const pages = useCasesSource.getPages(lang);

  if (lang === i18n.defaultLanguage) return pages;

  return mergeUseCasePagesWithEnglishFallback(
    pages,
    useCasesSource.getPages(i18n.defaultLanguage),
  );
}

export function getUseCasePageForLocale(
  slugs: string[],
  locale: SupportedLocale,
) {
  const lang = getUseCaseLangForLocale(locale);
  const page = useCasesSource.getPage(slugs, lang);

  if (page || lang === i18n.defaultLanguage) return page;

  return useCasesSource.getPage(slugs, i18n.defaultLanguage);
}
