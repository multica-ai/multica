import { loader } from "fumadocs-core/source";
import { defineI18n } from "fumadocs-core/i18n";
import { useCases } from "@/.source";

// Chinese ships first and is the default language so URLs stay prefix-free
// (`/use-cases/<slug>`). parser: 'dot' picks up `<slug>.zh.mdx` for ZH and
// will pick up `<slug>.en.mdx` for the English translation when it lands —
// at that point we swap `defaultLanguage` to 'en' and add a `[lang]` route.
export const i18n = defineI18n({
  languages: ["en", "zh"],
  defaultLanguage: "zh",
  hideLocale: "default-locale",
  parser: "dot",
});

export type UseCaseLang = (typeof i18n.languages)[number];

export const useCasesSource = loader({
  baseUrl: "/use-cases",
  source: useCases.toFumadocsSource(),
  i18n,
});
