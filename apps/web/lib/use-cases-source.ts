import { loader } from "fumadocs-core/source";
import { defineI18n } from "fumadocs-core/i18n";
import type { SupportedLocale } from "@multica/core/i18n";
import { useCases } from "@/.source";

// Use-case content still uses dot-suffixed MDX files (`<slug>.en.mdx` and
// `<slug>.zh.mdx`). The public route remains prefix-free; request locale is
// resolved through the same cookie/header path as the rest of the web app.
export const i18n = defineI18n({
  languages: ["en", "zh"],
  defaultLanguage: "en",
  hideLocale: "default-locale",
  parser: "dot",
});

export type UseCaseLang = (typeof i18n.languages)[number];

export function getUseCaseLangForLocale(locale: SupportedLocale): UseCaseLang {
  return locale === "zh-Hans" ? "zh" : "en";
}

export const useCasesSource = loader({
  baseUrl: "/usecases",
  source: useCases.toFumadocsSource(),
  i18n,
});
